package http

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/app"
)

// La web no guarda el refresh token donde JavaScript lo lea: lo pide en una
// cookie HttpOnly con esta cabecera (plan 17). Una cabecera propia obliga al
// navegador a preguntar antes (preflight), así otro sitio no puede forjar la
// petición; además la cookie es SameSite=Strict.
const (
	SessionTransportHeader = "X-Session-Transport"
	cookieTransport        = "cookie"
	RefreshCookieName      = "orbita_refresh"
	// refreshCookiePath: el navegador solo la manda a las rutas de sesión.
	refreshCookiePath = "/api/v1/auth"
)

// CookieOptions configura la cookie del refresh token.
type CookieOptions struct {
	// Secure va siempre en el servidor; en desarrollo (http://localhost) no.
	Secure bool
	MaxAge time.Duration
}

func usesCookie(c *gin.Context) bool {
	return c.GetHeader(SessionTransportHeader) == cookieTransport
}

func (h *Handler) setCookie(c *gin.Context, tokens app.Tokens) {
	ttl := tokens.RefreshExpiresIn
	if ttl <= 0 {
		ttl = h.cookie.MaxAge
	}
	h.writeCookie(c, tokens.RefreshToken, int(ttl.Seconds()))
}

func (h *Handler) clearCookie(c *gin.Context) {
	h.writeCookie(c, "", -1)
}

func (h *Handler) writeCookie(c *gin.Context, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    value,
		Path:     refreshCookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteStrictMode,
	})
}
