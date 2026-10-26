package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
)

const googleCookieName = "orbita_google_nonce"

func (h *Handler) googleConfig(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	apierror.OK(c, gin.H{"client_id": h.googleClientID})
}

// Custom headers require CORS preflight. In a browser the HttpOnly, SameSite
// cookie binds the Google nonce to it; a native app (no session header) keeps
// the nonce itself and sends it back. Either way the database makes it
// single-use.
func (h *Handler) googleChallenge(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	nonce, err := h.service.NewGoogleChallenge(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	if usesCookie(c) {
		h.writeGoogleCookie(c, nonce, int(app.GoogleChallengeTTL.Seconds()))
	}
	apierror.OK(c, gin.H{"nonce": nonce})
}

// googleSignIn: a browser's nonce comes in its cookie and it gets a web session
// (refresh token in a cookie); a native app sends the nonce in the body and gets
// a native session with the refresh token in the body, like login.
func (h *Handler) googleSignIn(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.ContentType() != "application/json" {
		apierror.Respond(c, apierror.ErrUnauthorized)
		return
	}
	web := usesCookie(c)
	var req struct {
		Nonce        string `json:"nonce"`
		Credential   string `json:"credential"`
		Password     string `json:"password"`
		MainCurrency string `json:"main_currency"`
		Timezone     string `json:"timezone"`
	}
	if !bind(c, &req) {
		return
	}
	nonce := req.Nonce
	if web {
		// The browser's nonce is only trusted from its own cookie.
		nonce, _ = c.Cookie(googleCookieName)
	}
	if nonce == "" {
		respondError(c, domain.ErrGoogleChallengeExpired)
		return
	}
	result, err := h.service.GoogleSignIn(c.Request.Context(), app.GoogleInput{
		Credential: req.Credential, Nonce: nonce, Password: req.Password,
		MainCurrency: req.MainCurrency, Timezone: req.Timezone, UserAgent: c.Request.UserAgent(), Web: web,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	if web {
		h.writeGoogleCookie(c, "", -1)
	}
	apierror.OK(c, toAuthResponse(h.deliverRefreshToken(c, result)))
}

func (h *Handler) writeGoogleCookie(c *gin.Context, nonce string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: googleCookieName, Value: nonce, Path: "/api/v1/auth/google",
		MaxAge: maxAge, HttpOnly: true, Secure: h.cookie.Secure, SameSite: http.SameSiteStrictMode,
	})
}

// googleDeleteAccount borra la cuenta de quien no tiene contraseña. Mismo reto que
// el inicio con Google: el navegador lo manda en su cookie y una app nativa en el cuerpo.
func (h *Handler) googleDeleteAccount(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.ContentType() != "application/json" {
		apierror.Respond(c, apierror.ErrUnauthorized)
		return
	}
	web := usesCookie(c)
	var req struct {
		Nonce      string `json:"nonce"`
		Credential string `json:"credential"`
	}
	if !bind(c, &req) {
		return
	}
	nonce := req.Nonce
	if web {
		nonce, _ = c.Cookie(googleCookieName)
	}
	if nonce == "" {
		respondError(c, domain.ErrGoogleChallengeExpired)
		return
	}
	principal, _ := auth.FromContext(c.Request.Context())
	if err := h.service.DeleteAccountWithGoogle(c.Request.Context(), principal.UserID, req.Credential, nonce); err != nil {
		respondError(c, err)
		return
	}
	if web {
		h.writeGoogleCookie(c, "", -1)
		h.clearCookie(c)
	}
	apierror.NoContent(c)
}
