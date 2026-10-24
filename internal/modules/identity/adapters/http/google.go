package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
)

const googleCookieName = "orbita_google_nonce"

func (h *Handler) googleConfig(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	apierror.OK(c, gin.H{"client_id": h.googleClientID})
}

// Custom headers require CORS preflight. The HttpOnly, SameSite cookie binds
// the Google nonce to this browser; the database makes it single-use.
func (h *Handler) googleChallenge(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !usesCookie(c) {
		apierror.Respond(c, apierror.ErrUnauthorized)
		return
	}
	nonce, err := h.service.NewGoogleChallenge(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	h.writeGoogleCookie(c, nonce, int(app.GoogleChallengeTTL.Seconds()))
	apierror.OK(c, gin.H{"nonce": nonce})
}

func (h *Handler) googleSignIn(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !usesCookie(c) || c.ContentType() != "application/json" {
		apierror.Respond(c, apierror.ErrUnauthorized)
		return
	}
	nonce, _ := c.Cookie(googleCookieName)
	if nonce == "" {
		respondError(c, domain.ErrGoogleChallengeExpired)
		return
	}
	var req struct {
		Credential   string `json:"credential"`
		Password     string `json:"password"`
		MainCurrency string `json:"main_currency"`
		Timezone     string `json:"timezone"`
	}
	if !bind(c, &req) {
		return
	}
	result, err := h.service.GoogleSignIn(c.Request.Context(), app.GoogleInput{
		Credential: req.Credential, Nonce: nonce, Password: req.Password,
		MainCurrency: req.MainCurrency, Timezone: req.Timezone, UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		respondError(c, err)
		return
	}
	h.writeGoogleCookie(c, "", -1)
	apierror.OK(c, toAuthResponse(h.deliverRefreshToken(c, result)))
}

func (h *Handler) writeGoogleCookie(c *gin.Context, nonce string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: googleCookieName, Value: nonce, Path: "/api/v1/auth/google",
		MaxAge: maxAge, HttpOnly: true, Secure: h.cookie.Secure, SameSite: http.SameSiteStrictMode,
	})
}
