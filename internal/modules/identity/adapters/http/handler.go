// Package http traduce HTTP ⇄ casos de uso de identity. Sin reglas de negocio:
// parsear, llamar al servicio, responder.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
)

// Service son los casos de uso que usa el handler.
type Service interface {
	Register(ctx context.Context, in app.RegisterInput) (app.AuthResult, error)
	Login(ctx context.Context, in app.LoginInput) (app.AuthResult, error)
	Refresh(ctx context.Context, refreshToken string) (app.Tokens, error)
	Logout(ctx context.Context, refreshToken string) error
	Me(ctx context.Context, userID uuid.UUID) (domain.User, error)
	RequestPasswordReset(ctx context.Context, email string) error
	VerifyResetCode(ctx context.Context, email, code string) error
	ResetPassword(ctx context.Context, email, code, newPassword string) error
	DeleteAccount(ctx context.Context, userID uuid.UUID, password string) error
}

// Códigos propios del módulo.
const (
	codeEmailAlreadyRegistered = "EMAIL_ALREADY_REGISTERED"
	codeInvalidCredentials     = "INVALID_CREDENTIALS"
	codeSessionExpired         = "SESSION_EXPIRED"
	codeResetCodeInvalid       = "RESET_CODE_INVALID"
)

var (
	errEmailTaken         = apierror.New(http.StatusConflict, codeEmailAlreadyRegistered, "Ese correo ya tiene una cuenta. Inicia sesión.")
	errInvalidCredentials = apierror.New(http.StatusUnauthorized, codeInvalidCredentials, "Correo o contraseña incorrectos.")
	errSessionExpired     = apierror.New(http.StatusUnauthorized, codeSessionExpired, "Tu sesión terminó. Inicia sesión de nuevo.")
	errResetCodeInvalid   = apierror.New(http.StatusUnprocessableEntity, codeResetCodeInvalid, "El código no es válido o ya venció. Pide uno nuevo.")
)

// Handler atiende las rutas de autenticación.
type Handler struct {
	service Service
	cookie  CookieOptions
}

// NewHandler crea el handler.
func NewHandler(service Service, cookie CookieOptions) *Handler {
	return &Handler{service: service, cookie: cookie}
}

// Routes monta las rutas. public no exige token; limited son las públicas con
// el límite estrecho de LOGIN_RATE_LIMIT_PER_MINUTE; protected exige token.
func (h *Handler) Routes(public, limited, protected gin.IRoutes) {
	limited.POST("/auth/register", h.register)
	limited.POST("/auth/login", h.login)
	limited.POST("/auth/refresh", h.refresh)
	// Recuperar la contraseña (plan 11): el mismo límite por IP que el inicio de sesión.
	limited.POST("/auth/forgot-password", h.forgotPassword)
	limited.POST("/auth/verify-reset-code", h.verifyResetCode)
	limited.POST("/auth/reset-password", h.resetPassword)
	public.POST("/auth/logout", h.logout)
	protected.GET("/me", h.me)
	protected.DELETE("/me", h.deleteAccount)
}

type registerRequest struct {
	Email        string `json:"email"`
	Name         string `json:"name"`
	Password     string `json:"password"`
	MainCurrency string `json:"main_currency"`
	Timezone     string `json:"timezone"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type userResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      *string   `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// RefreshToken va vacío (y se omite) cuando viaja en la cookie de la web.
type tokensResponse struct {
	AccessToken          string `json:"access_token"`
	AccessTokenExpiresIn int    `json:"access_token_expires_in"`
	RefreshToken         string `json:"refresh_token,omitempty"`
}

type authResponse struct {
	User   userResponse   `json:"user"`
	Tokens tokensResponse `json:"tokens"`
}

func (h *Handler) register(c *gin.Context) {
	var req registerRequest
	if !bind(c, &req) {
		return
	}
	result, err := h.service.Register(c.Request.Context(), app.RegisterInput{
		Email: req.Email, Name: req.Name, Password: req.Password, MainCurrency: req.MainCurrency,
		Timezone: req.Timezone, UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.Created(c, toAuthResponse(h.deliverRefreshToken(c, result)))
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if !bind(c, &req) {
		return
	}
	result, err := h.service.Login(c.Request.Context(), app.LoginInput{
		Email: req.Email, Password: req.Password, UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toAuthResponse(h.deliverRefreshToken(c, result)))
}

func (h *Handler) refresh(c *gin.Context) {
	token, ok := h.refreshTokenOf(c)
	if !ok {
		return
	}
	tokens, err := h.service.Refresh(c.Request.Context(), token)
	if err != nil {
		if errors.Is(err, domain.ErrSessionExpired) && usesCookie(c) {
			h.clearCookie(c)
		}
		respondError(c, err)
		return
	}
	if usesCookie(c) {
		h.setCookie(c, tokens.RefreshToken)
		tokens.RefreshToken = ""
	}
	apierror.OK(c, gin.H{"tokens": toTokens(tokens)})
}

func (h *Handler) logout(c *gin.Context) {
	token, ok := h.refreshTokenOf(c)
	if !ok {
		return
	}
	if usesCookie(c) {
		h.clearCookie(c)
	}
	if err := h.service.Logout(c.Request.Context(), token); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

// refreshTokenOf lee el token de renovación: de la cookie si la petición es de
// la web, del cuerpo si no.
func (h *Handler) refreshTokenOf(c *gin.Context) (string, bool) {
	if usesCookie(c) {
		token, _ := c.Cookie(RefreshCookieName)
		return token, true
	}
	var req refreshRequest
	if !bind(c, &req) {
		return "", false
	}
	return req.RefreshToken, true
}

// deliverRefreshToken pone el token en la cookie cuando la petición es de la web
// y lo saca del cuerpo, para que JavaScript nunca lo vea.
func (h *Handler) deliverRefreshToken(c *gin.Context, result app.AuthResult) app.AuthResult {
	if usesCookie(c) {
		h.setCookie(c, result.Tokens.RefreshToken)
		result.Tokens.RefreshToken = ""
	}
	return result
}

func (h *Handler) me(c *gin.Context) {
	principal, _ := auth.FromContext(c.Request.Context())
	user, err := h.service.Me(c.Request.Context(), principal.UserID)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toUser(user))
}

// bind lee el cuerpo JSON. Los campos que no conoce se ignoran: una app más
// nueva puede mandar datos que esta versión del API todavía no usa.
func bind(c *gin.Context, target any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierror.Respond(c, err)
			return false
		}
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "body", Code: domain.CodeInvalidFormat}))
		return false
	}
	return true
}

func respondError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	var tooMany *domain.TooManyAttemptsError
	switch {
	case errors.As(err, &validation):
		fields := make([]apierror.FieldError, 0, len(validation.Fields))
		for _, f := range validation.Fields {
			fields = append(fields, apierror.FieldError{Field: f.Field, Code: f.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.As(err, &tooMany):
		c.Header("Retry-After", strconv.Itoa(int(tooMany.RetryAfter.Seconds())+1))
		apierror.Respond(c, apierror.ErrTooManyRequests)
	case errors.Is(err, domain.ErrEmailTaken):
		apierror.Respond(c, errEmailTaken)
	case errors.Is(err, domain.ErrInvalidCredentials):
		apierror.Respond(c, errInvalidCredentials)
	case errors.Is(err, domain.ErrSessionExpired):
		apierror.Respond(c, errSessionExpired)
	case errors.Is(err, domain.ErrResetCodeInvalid):
		apierror.Respond(c, errResetCodeInvalid)
	case errors.Is(err, domain.ErrNotFound):
		apierror.Respond(c, apierror.ErrUnauthorized)
	default:
		apierror.Respond(c, err)
	}
}

func toAuthResponse(result app.AuthResult) authResponse {
	return authResponse{User: toUser(result.User), Tokens: toTokens(result.Tokens)}
}

func toUser(user domain.User) userResponse {
	return userResponse{ID: user.ID, Email: user.Email, Name: user.Name, CreatedAt: user.CreatedAt.UTC()}
}

func toTokens(tokens app.Tokens) tokensResponse {
	return tokensResponse{
		AccessToken:          tokens.AccessToken,
		AccessTokenExpiresIn: int(tokens.ExpiresIn.Seconds()),
		RefreshToken:         tokens.RefreshToken,
	}
}

type resetRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

// forgotPassword responde 204 siempre, exista o no el correo.
func (h *Handler) forgotPassword(c *gin.Context) {
	var req resetRequest
	if !bind(c, &req) {
		return
	}
	if err := h.service.RequestPasswordReset(c.Request.Context(), req.Email); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

func (h *Handler) verifyResetCode(c *gin.Context) {
	var req resetRequest
	if !bind(c, &req) {
		return
	}
	if err := h.service.VerifyResetCode(c.Request.Context(), req.Email, req.Code); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

func (h *Handler) resetPassword(c *gin.Context) {
	var req resetRequest
	if !bind(c, &req) {
		return
	}
	if err := h.service.ResetPassword(c.Request.Context(), req.Email, req.Code, req.NewPassword); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

type deleteAccountRequest struct {
	Password string `json:"password"`
}

// deleteAccount borra la cuenta y todos sus datos; las sesiones se van con ella.
func (h *Handler) deleteAccount(c *gin.Context) {
	var req deleteAccountRequest
	if !bind(c, &req) {
		return
	}
	principal, _ := auth.FromContext(c.Request.Context())
	if err := h.service.DeleteAccount(c.Request.Context(), principal.UserID, req.Password); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}
