package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/currency"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
)

// DefaultTimezone es la zona de un perfil cuando la app no envía una válida.
const DefaultTimezone = "America/Lima"

// Motivos por los que se revoca una sesión (sessions.revoked_reason).
const (
	revokedLogout = "logout"
	revokedReuse  = "reuse"
)

const (
	refreshTokenBytes = 32
	userAgentMaxRunes = 255
)

// Config son los tiempos de vida de los tokens.
type Config struct {
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// Deps son los puertos que necesita el servicio.
type Deps struct {
	Users    Users
	Sessions Sessions
	Hasher   PasswordHasher
	Tokens   AccessTokens
	Tx       Transactor
	Clock    clock.Clock
	Config   Config
}

// Service son los casos de uso de cuentas y sesiones.
type Service struct {
	deps      Deps
	attempts  *loginAttempts
	hooks     []RegistrationHook
	dummyHash string
}

// NewService crea el servicio. Calcula un hash ficticio para que iniciar sesión
// con un correo que no existe tarde lo mismo que con uno que existe.
func NewService(deps Deps) (*Service, error) {
	dummy, err := deps.Hasher.Hash("not-a-real-password")
	if err != nil {
		return nil, fmt.Errorf("compute dummy hash: %w", err)
	}
	return &Service{deps: deps, attempts: newLoginAttempts(), dummyHash: dummy}, nil
}

// OnUserRegistered agrega un gancho al alta. Corren en el orden en que se agregan.
func (s *Service) OnUserRegistered(hook RegistrationHook) {
	s.hooks = append(s.hooks, hook)
}

// Tokens son los que recibe la app al entrar o renovar.
type Tokens struct {
	AccessToken  string
	ExpiresIn    time.Duration
	RefreshToken string
}

// AuthResult es la respuesta de registro e inicio de sesión.
type AuthResult struct {
	User   domain.User
	Tokens Tokens
}

// RegisterInput son los datos del formulario de registro.
type RegisterInput struct {
	Email        string
	Password     string
	MainCurrency string
	Timezone     string
	UserAgent    string
}

// Register crea la cuenta, su primera sesión y todo lo que los ganchos agregan,
// en una sola transacción.
func (s *Service) Register(ctx context.Context, in RegisterInput) (AuthResult, error) {
	email := domain.NormalizeEmail(in.Email)
	mainCurrency := in.MainCurrency
	if mainCurrency == "" {
		mainCurrency = currency.Default
	}

	var problems domain.ValidationError
	domain.ValidateEmail(email, &problems)
	domain.ValidateNewPassword(in.Password, &problems)
	if !currency.IsSupported(mainCurrency) {
		problems.Add("main_currency", domain.CodeCurrencyNotSupported)
	}
	if err := problems.OrNil(); err != nil {
		return AuthResult{}, err
	}

	hash, err := s.deps.Hasher.Hash(in.Password)
	if err != nil {
		return AuthResult{}, fmt.Errorf("hash password: %w", err)
	}

	now := s.deps.Clock.Now()
	user := domain.User{ID: ids.New(), Email: email, PasswordHash: hash, CreatedAt: now}
	var tokens Tokens
	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Users.Create(ctx, user); err != nil {
			return err
		}
		newUser := NewUser{ID: user.ID, Email: email, MainCurrency: mainCurrency, Timezone: validTimezone(in.Timezone)}
		for _, hook := range s.hooks {
			if err := hook(ctx, newUser); err != nil {
				return fmt.Errorf("registration hook: %w", err)
			}
		}
		tokens, err = s.openSession(ctx, user.ID, in.UserAgent, now)
		return err
	})
	if err != nil {
		return AuthResult{}, err
	}
	logger.From(ctx).Info("security: user registered", "user_id", user.ID)
	return AuthResult{User: user, Tokens: tokens}, nil
}

// LoginInput son los datos de Iniciar sesión.
type LoginInput struct {
	Email     string
	Password  string
	UserAgent string
}

// Login comprueba correo y contraseña y abre una sesión. "No existe" y
// "contraseña equivocada" dan el mismo error y tardan lo mismo.
func (s *Service) Login(ctx context.Context, in LoginInput) (AuthResult, error) {
	email := domain.NormalizeEmail(in.Email)
	now := s.deps.Clock.Now()
	if wait := s.attempts.wait(email, now); wait > 0 {
		return AuthResult{}, &domain.TooManyAttemptsError{RetryAfter: wait}
	}

	user, err := s.deps.Users.ByEmail(ctx, email)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		_, _ = s.deps.Hasher.Verify(in.Password, s.dummyHash)
		return AuthResult{}, s.loginFailed(ctx, email, now)
	case err != nil:
		return AuthResult{}, err
	}

	ok, err := s.deps.Hasher.Verify(in.Password, user.PasswordHash)
	if err != nil {
		return AuthResult{}, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return AuthResult{}, s.loginFailed(ctx, email, now)
	}
	s.attempts.succeeded(email)

	var tokens Tokens
	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Users.TouchLastLogin(ctx, user.ID); err != nil {
			return err
		}
		tokens, err = s.openSession(ctx, user.ID, in.UserAgent, now)
		return err
	})
	if err != nil {
		return AuthResult{}, err
	}
	logger.From(ctx).Info("security: login succeeded", "user_id", user.ID)
	return AuthResult{User: user, Tokens: tokens}, nil
}

func (s *Service) loginFailed(ctx context.Context, email string, now time.Time) error {
	s.attempts.failed(email, now)
	logger.From(ctx).Warn("security: login failed")
	return domain.ErrInvalidCredentials
}

// Refresh cambia un token de renovación por uno nuevo (rotación). Si llega uno
// ya usado, alguien más lo tiene: se revoca la sesión entera.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	now := s.deps.Clock.Now()
	var tokens Tokens
	var reused *LockedRefreshToken
	err := s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		stored, err := s.deps.Sessions.LockRefreshToken(ctx, hashToken(refreshToken))
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrSessionExpired
		}
		if err != nil {
			return err
		}
		if stored.Used {
			// Se guarda la revocación (sin error) y luego se responde 401.
			reused = &stored
			return s.deps.Sessions.Revoke(ctx, stored.SessionID, revokedReuse)
		}
		if stored.SessionRevoked || !now.Before(stored.ExpiresAt) {
			return domain.ErrSessionExpired
		}
		if err := s.deps.Sessions.MarkRefreshTokenUsed(ctx, stored.ID); err != nil {
			return err
		}
		if err := s.deps.Sessions.Touch(ctx, stored.SessionID); err != nil {
			return err
		}
		tokens, err = s.issueTokens(ctx, stored.UserID, stored.SessionID, now)
		return err
	})
	if err != nil {
		return Tokens{}, err
	}
	if reused != nil {
		logger.From(ctx).Warn("security: refresh token reused, session revoked",
			"user_id", reused.UserID, "session_id", reused.SessionID)
		return Tokens{}, domain.ErrSessionExpired
	}
	return tokens, nil
}

// Logout cierra la sesión del token de renovación. Nunca falla por un token
// desconocido: cerrar sesión siempre debe funcionar.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	return s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		stored, err := s.deps.Sessions.LockRefreshToken(ctx, hashToken(refreshToken))
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		logger.From(ctx).Info("security: logout", "user_id", stored.UserID)
		return s.deps.Sessions.Revoke(ctx, stored.SessionID, revokedLogout)
	})
}

// Me devuelve la cuenta de quien hace la petición.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (domain.User, error) {
	return s.deps.Users.ByID(ctx, userID)
}

// Authenticate valida un access token y que su sesión siga viva: cerrar sesión
// o eliminar la cuenta corta el acceso al instante, sin esperar a que venza.
func (s *Service) Authenticate(ctx context.Context, accessToken string) (auth.Principal, error) {
	claims, err := s.deps.Tokens.Verify(accessToken, s.deps.Clock.Now())
	if err != nil {
		return auth.Principal{}, auth.ErrUnauthorized
	}
	active, err := s.deps.Sessions.IsActive(ctx, claims.SessionID, claims.UserID)
	if err != nil {
		return auth.Principal{}, fmt.Errorf("check session: %w", err)
	}
	if !active {
		return auth.Principal{}, auth.ErrUnauthorized
	}
	return auth.Principal{UserID: claims.UserID, SessionID: claims.SessionID}, nil
}

func (s *Service) openSession(ctx context.Context, userID uuid.UUID, userAgent string, now time.Time) (Tokens, error) {
	sessionID := ids.New()
	if err := s.deps.Sessions.Create(ctx, Session{ID: sessionID, UserID: userID, UserAgent: truncate(userAgent, userAgentMaxRunes)}); err != nil {
		return Tokens{}, err
	}
	return s.issueTokens(ctx, userID, sessionID, now)
}

func (s *Service) issueTokens(ctx context.Context, userID, sessionID uuid.UUID, now time.Time) (Tokens, error) {
	access, err := s.deps.Tokens.Issue(AccessClaims{UserID: userID, SessionID: sessionID}, now)
	if err != nil {
		return Tokens{}, fmt.Errorf("issue access token: %w", err)
	}
	refresh, err := newRefreshToken()
	if err != nil {
		return Tokens{}, err
	}
	err = s.deps.Sessions.CreateRefreshToken(ctx, RefreshToken{
		ID:        ids.New(),
		SessionID: sessionID,
		Hash:      hashToken(refresh),
		ExpiresAt: now.Add(s.deps.Config.RefreshTokenTTL),
	})
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, ExpiresIn: s.deps.Config.AccessTokenTTL, RefreshToken: refresh}, nil
}

func newRefreshToken() (string, error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func validTimezone(name string) string {
	if _, err := dates.LoadLocation(name); err != nil {
		return DefaultTimezone
	}
	return name
}

func truncate(text string, maxRunes int) string {
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	return string([]rune(text)[:maxRunes])
}
