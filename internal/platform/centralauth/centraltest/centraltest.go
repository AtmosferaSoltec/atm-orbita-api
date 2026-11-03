// Package centraltest arma, para las pruebas de integración, lo que en producción
// es atm-auth-api: firma tokens RS256 de personas de Orbita y las da de alta con
// los mismos ganchos que main.go. Así las pruebas recorren el camino real de
// centralauth en vez de un login falso.
package centraltest

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/centralauth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

const issuer = "atm-auth-api"

// Harness firma tokens y autentica con ellos.
type Harness struct {
	Users centralauth.Users
	key   *rsa.PrivateKey
	auth  *centralauth.Authenticator
}

// New crea el Harness. setup son los ganchos de alta de main.go (perfil y libro);
// onDelete, los de borrado (archivos).
func New(t *testing.T, runner tx.Runner, setup centralauth.Setup, onDelete ...centralauth.DeletionHook) *Harness {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	verifier, err := centralauth.NewVerifier(base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), issuer)
	require.NoError(t, err)
	users := centralauth.Users{Tx: runner, Setup: setup, OnDelete: onDelete, MainCurrency: "PEN", Timezone: "America/Lima"}
	return &Harness{Users: users, key: key, auth: &centralauth.Authenticator{
		Verifier: verifier, Provisioner: users, Fallback: rejecting{},
	}}
}

// Authenticator es el que va en auth.Require.
func (h *Harness) Authenticator() auth.Authenticator { return h.auth }

// SignUp da de alta a una persona con esa moneda principal y zona (vacías: las
// de siempre, PEN y America/Lima), y devuelve su id y un access token.
func (h *Harness) SignUp(t *testing.T, email, mainCurrency, timezone string) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	people := h.Users
	if mainCurrency != "" {
		people.MainCurrency = mainCurrency
	}
	if timezone != "" {
		people.Timezone = timezone
	}
	require.NoError(t, people.Provision(context.Background(), id, email, ""))
	return id, h.Token(t, id, email)
}

// Token firma el access token de una persona.
func (h *Harness) Token(t *testing.T, id uuid.UUID, email string) string {
	t.Helper()
	claims := centralauth.Claims{Role: "user", System: centralauth.SystemCode, Email: email, RegisteredClaims: jwt.RegisteredClaims{
		Subject: id.String(), Issuer: issuer, ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
	}}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(h.key)
	require.NoError(t, err)
	return signed
}

// rejecting es el "autenticador propio" que ya no existe: todo lo que no sea de auth se rechaza.
type rejecting struct{}

func (rejecting) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{}, auth.ErrUnauthorized
}
