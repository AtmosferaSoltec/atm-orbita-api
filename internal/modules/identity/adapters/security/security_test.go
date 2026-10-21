package security

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
)

// Parámetros bajos: la prueba mide la lógica, no la resistencia del hash.
var fastHasher = Argon2Hasher{Params: Argon2Params{MemoryKiB: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}}

func TestTheHashAcceptsTheRightPasswordOnly(t *testing.T) {
	hash, err := fastHasher.Hash("vidanueva123")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=1024,t=1,p=1$"))

	ok, err := fastHasher.Verify("vidanueva123", hash)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = fastHasher.Verify("vidanueva124", hash)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestTwoHashesOfTheSamePasswordDiffer(t *testing.T) {
	a, _ := fastHasher.Hash("same password")
	b, _ := fastHasher.Hash("same password")
	assert.NotEqual(t, a, b)
}

func TestAHashKeepsItsOwnParameters(t *testing.T) {
	// Un hash viejo sigue sirviendo después de cambiar los parámetros.
	hash, _ := fastHasher.Hash("vidanueva123")
	ok, err := Argon2Hasher{Params: DefaultArgon2Params}.Verify("vidanueva123", hash)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestAMalformedHashIsAnError(t *testing.T) {
	_, err := fastHasher.Verify("x", "$2a$10$bcrypt-hash")
	assert.Error(t, err)
}

var secret = []byte(strings.Repeat("k", 64))

func TestAnAccessTokenCarriesTheUserAndTheSession(t *testing.T) {
	tokens := JWTTokens{Secret: secret, TTL: 15 * time.Minute}
	want := app.AccessClaims{UserID: ids.New(), SessionID: ids.New()}
	now := time.Now()

	token, err := tokens.Issue(want, now)
	require.NoError(t, err)
	got, err := tokens.Verify(token, now.Add(14*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestRejectedAccessTokens(t *testing.T) {
	tokens := JWTTokens{Secret: secret, TTL: 15 * time.Minute}
	claims := app.AccessClaims{UserID: ids.New(), SessionID: ids.New()}
	now := time.Now()
	valid, _ := tokens.Issue(claims, now)

	expired, _ := tokens.Issue(claims, now.Add(-16*time.Minute))
	otherSecret, _ := JWTTokens{Secret: []byte(strings.Repeat("o", 64)), TTL: time.Hour}.Issue(claims, now)
	parts := strings.Split(valid, ".")
	tampered := parts[0] + "." + parts[1] + "x." + parts[2]

	none := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": claims.UserID.String(), "sid": claims.SessionID.String(), "exp": now.Add(time.Hour).Unix(),
	})
	unsigned, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	hs512 := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.MapClaims{
		"sub": claims.UserID.String(), "sid": claims.SessionID.String(), "exp": now.Add(time.Hour).Unix(),
	})
	otherAlg, _ := hs512.SignedString(secret)

	for name, token := range map[string]string{
		"expired": expired, "other secret": otherSecret, "tampered": tampered,
		"alg none": unsigned, "HS512": otherAlg, "garbage": "not-a-jwt",
	} {
		_, err := tokens.Verify(token, now)
		assert.ErrorIs(t, err, auth.ErrUnauthorized, name)
	}
}
