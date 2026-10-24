package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
)

type googleKeyTransport struct{ key *rsa.PublicKey }

func (r googleKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := json.Marshal(map[string]any{"keys": []any{map[string]string{
		"kty": "RSA", "kid": "test-key", "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(r.key.N.Bytes()), "e": "AQAB",
	}}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Cache-Control": {"max-age=3600"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
}

func TestGoogleVerifierChecksSignatureAudienceIssuerExpiryAndNonce(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	validator, err := idtoken.NewValidator(context.Background(), option.WithHTTPClient(&http.Client{Transport: googleKeyTransport{&key.PublicKey}}))
	require.NoError(t, err)
	v := GoogleVerifier{ClientID: "orbita.apps.googleusercontent.com", validator: validator}
	for _, test := range []struct {
		name     string
		mutate   func(jwt.MapClaims)
		wrongKey bool
	}{
		{name: "valid"},
		{name: "wrong audience", mutate: func(c jwt.MapClaims) { c["aud"] = "another-client" }},
		{name: "wrong issuer", mutate: func(c jwt.MapClaims) { c["iss"] = "https://attacker.test" }},
		{name: "expired", mutate: func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }},
		{name: "future issue time", mutate: func(c jwt.MapClaims) { c["iat"] = time.Now().Add(time.Hour).Unix() }},
		{name: "wrong nonce", mutate: func(c jwt.MapClaims) { c["nonce"] = "another-browser" }},
		{name: "missing nonce", mutate: func(c jwt.MapClaims) { delete(c, "nonce") }},
		{name: "unverified email", mutate: func(c jwt.MapClaims) { c["email_verified"] = false }},
		{name: "missing subject", mutate: func(c jwt.MapClaims) { delete(c, "sub") }},
		{name: "bad signature", wrongKey: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := jwt.MapClaims{"aud": v.ClientID, "iss": "https://accounts.google.com", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "sub": "google-account-123", "email": "ana@gmail.com", "email_verified": true, "name": "Ana", "nonce": "browser-nonce"}
			if test.mutate != nil {
				test.mutate(claims)
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			token.Header["kid"] = "test-key"
			signingKey := key
			if test.wrongKey {
				signingKey, err = rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
			}
			raw, err := token.SignedString(signingKey)
			require.NoError(t, err)
			account, err := v.Verify(context.Background(), raw, "browser-nonce")
			if test.name == "valid" {
				require.NoError(t, err)
				assert.Equal(t, "google-account-123", account.Subject)
				assert.Equal(t, "ana@gmail.com", account.Email)
			} else {
				assert.ErrorIs(t, err, domain.ErrGoogleInvalid)
			}
		})
	}
}
