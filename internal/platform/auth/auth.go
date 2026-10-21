// Package auth lleva en el contexto quién hace la petición. El user_id sale solo
// de aquí (del token): ningún módulo lo acepta del cuerpo ni de la URL.
package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
)

// Principal es la persona autenticada y la sesión (dispositivo) desde la que llega.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

// ErrUnauthorized: token ausente, inválido, vencido o de una sesión revocada.
var ErrUnauthorized = errors.New("unauthorized")

// Authenticator valida un access token. Lo implementa el módulo identity.
type Authenticator interface {
	Authenticate(ctx context.Context, accessToken string) (Principal, error)
}

type contextKey struct{}

// WithPrincipal guarda quién hace la petición.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// FromContext devuelve quién hace la petición. Solo existe en rutas protegidas.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

// Require exige "Authorization: Bearer <access token>". Todo lo que se monta
// detrás de él es privado: olvidar algo deja la ruta protegida, no abierta.
func Require(authenticator Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			apierror.Respond(c, apierror.ErrUnauthorized)
			return
		}
		principal, err := authenticator.Authenticate(c.Request.Context(), token)
		if errors.Is(err, ErrUnauthorized) {
			apierror.Respond(c, apierror.ErrUnauthorized)
			return
		}
		if err != nil {
			apierror.Respond(c, err)
			return
		}
		c.Request = c.Request.WithContext(WithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
}

func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}
