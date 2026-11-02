package centralauth

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
)

type fixture struct {
	key *rsa.PrivateKey
	v   *Verifier
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	b64 := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	v, err := NewVerifier(b64, "atm-auth-api")
	require.NoError(t, err)
	return fixture{key: key, v: v}
}

func (f fixture) token(t *testing.T, c Claims) string {
	t.Helper()
	c.Issuer = "atm-auth-api"
	if c.ExpiresAt == nil {
		c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Minute))
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodRS256, c).SignedString(f.key)
	require.NoError(t, err)
	return s
}

type fakeProvisioner struct{ calls []uuid.UUID }

func (p *fakeProvisioner) Provision(_ context.Context, id uuid.UUID, _, _ string) error {
	p.calls = append(p.calls, id)
	return nil
}

type legacy struct{ called bool }

func (l *legacy) Authenticate(context.Context, string) (auth.Principal, error) {
	l.called = true
	return auth.Principal{UserID: uuid.New()}, nil
}

func TestAcceptsAnOrbitaUserAndProvisionsOnce(t *testing.T) {
	f := newFixture(t)
	prov, fb := &fakeProvisioner{}, &legacy{}
	a := &Authenticator{Verifier: f.v, Provisioner: prov, Fallback: fb}
	id := uuid.New()
	tok := f.token(t, Claims{Role: "user", System: "orbita", Email: "A@B.co", RegisteredClaims: jwt.RegisteredClaims{Subject: id.String()}})

	for range 3 {
		p, err := a.Authenticate(context.Background(), tok)
		require.NoError(t, err)
		assert.Equal(t, id, p.UserID)
	}
	assert.Len(t, prov.calls, 1, "the first sight of a person provisions them, later requests do not")
	assert.False(t, fb.called)
}

func TestRejectsWrongRoleSystemSignatureAndExpiry(t *testing.T) {
	f := newFixture(t)
	a := &Authenticator{Verifier: f.v, Provisioner: &fakeProvisioner{}, Fallback: &legacy{}}
	sub := jwt.RegisteredClaims{Subject: uuid.NewString()}
	other := newFixture(t)
	expired := jwt.RegisteredClaims{Subject: sub.Subject, ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))}

	for name, tok := range map[string]string{
		"service token":  f.token(t, Claims{Role: "service", System: "orbita", RegisteredClaims: sub}),
		"another system": f.token(t, Claims{Role: "user", System: "pulso", RegisteredClaims: sub}),
		"foreign key":    other.token(t, Claims{Role: "user", System: "orbita", Email: "a@b.co", RegisteredClaims: sub}),
		"not a uuid":     f.token(t, Claims{Role: "user", System: "orbita", RegisteredClaims: jwt.RegisteredClaims{Subject: "admin"}}),
		"expired":        f.token(t, Claims{Role: "user", System: "orbita", RegisteredClaims: expired}),
	} {
		_, err := a.Authenticate(context.Background(), tok)
		assert.ErrorIs(t, err, auth.ErrUnauthorized, name)
	}
}

func TestOwnTokensGoToTheLegacyAuthenticator(t *testing.T) {
	f := newFixture(t)
	fb := &legacy{}
	a := &Authenticator{Verifier: f.v, Provisioner: &fakeProvisioner{}, Fallback: fb}
	hs, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: uuid.NewString()}).SignedString([]byte("secret"))
	require.NoError(t, err)
	_, err = a.Authenticate(context.Background(), hs)
	require.NoError(t, err)
	assert.True(t, fb.called)
}

func TestAnHS256TokenSignedWithThePublicKeyIsRejected(t *testing.T) {
	f := newFixture(t)
	der, _ := x509.MarshalPKIXPublicKey(&f.key.PublicKey)
	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{Role: "user", System: "orbita",
		RegisteredClaims: jwt.RegisteredClaims{Subject: uuid.NewString(), Issuer: "atm-auth-api", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}}).SignedString(der)
	require.NoError(t, err)
	_, verr := f.v.Verify(forged)
	assert.ErrorIs(t, verr, auth.ErrUnauthorized)
}
