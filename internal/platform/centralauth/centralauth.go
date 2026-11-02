// Package centralauth acepta los access tokens de atm-auth-api (RS256), que ya
// no pasan por el módulo identity. Convive con los tokens propios (HS256) hasta
// que los usuarios se migren: el algoritmo del encabezado decide quién valida.
package centralauth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
)

// SystemCode es el código de Orbita en atm-auth-api.
const SystemCode = "orbita"

// Claims son las que usa Orbita del token de atm-auth-api.
type Claims struct {
	Role   string `json:"role"`
	System string `json:"system"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	jwt.RegisteredClaims
}

// Verifier valida firma, emisor y vigencia. Solo acepta RS256.
type Verifier struct {
	key    *rsa.PublicKey
	issuer string
}

// NewVerifier lee la llave pública (PEM en base64, la misma AUTH_PUBLIC_KEY_BASE64
// de los demás servicios).
func NewVerifier(publicKeyBase64, issuer string) (*Verifier, error) {
	if issuer == "" {
		return nil, errors.New("centralauth: issuer is required")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyBase64))
	if err != nil {
		return nil, fmt.Errorf("centralauth: public key is not base64: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("centralauth: public key is not PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("centralauth: parse public key: %w", err)
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("centralauth: public key is not RSA")
	}
	return &Verifier{key: key, issuer: issuer}, nil
}

// Verify devuelve las claims de un token válido, o auth.ErrUnauthorized.
func (v *Verifier) Verify(token string) (Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return v.key, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(v.issuer), jwt.WithExpirationRequired())
	if err != nil {
		return Claims{}, auth.ErrUnauthorized
	}
	return claims, nil
}

// IsCentral dice si el token se firmó con RS256 (sin validarlo todavía).
func IsCentral(token string) bool {
	header, _, found := strings.Cut(token, ".")
	if !found {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(header)
	return err == nil && strings.Contains(string(raw), `"RS256"`)
}

// Provisioner crea lo que Orbita necesita de una persona la primera vez que la ve.
type Provisioner interface {
	Provision(ctx context.Context, userID uuid.UUID, email, name string) error
}

// Authenticator valida tokens de atm-auth-api y, si no es uno, delega en el
// autenticador propio.
type Authenticator struct {
	Verifier    *Verifier
	Provisioner Provisioner
	Fallback    auth.Authenticator

	mu   sync.Mutex
	seen map[uuid.UUID]struct{}
}

func (a *Authenticator) Authenticate(ctx context.Context, token string) (auth.Principal, error) {
	if a.Verifier == nil || !IsCentral(token) {
		return a.Fallback.Authenticate(ctx, token)
	}
	claims, err := a.Verifier.Verify(token)
	if err != nil {
		return auth.Principal{}, err
	}
	// Solo tokens de personas de Orbita: ni de servicio ni de otro sistema.
	userID, perr := uuid.Parse(claims.Subject)
	if claims.Role != "user" || claims.System != SystemCode || perr != nil {
		return auth.Principal{}, auth.ErrUnauthorized
	}
	if err := a.ensure(ctx, userID, claims); err != nil {
		return auth.Principal{}, err
	}
	return auth.Principal{UserID: userID}, nil
}

func (a *Authenticator) ensure(ctx context.Context, userID uuid.UUID, claims Claims) error {
	a.mu.Lock()
	_, ok := a.seen[userID]
	a.mu.Unlock()
	if ok {
		return nil
	}
	if err := a.Provisioner.Provision(ctx, userID, strings.ToLower(claims.Email), claims.Name); err != nil {
		return fmt.Errorf("provision user: %w", err)
	}
	a.mu.Lock()
	if a.seen == nil || len(a.seen) > 50_000 {
		a.seen = map[uuid.UUID]struct{}{}
	}
	a.seen[userID] = struct{}{}
	a.mu.Unlock()
	return nil
}
