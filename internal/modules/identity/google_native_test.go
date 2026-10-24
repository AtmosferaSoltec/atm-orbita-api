package identity_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity"
)

// Google from a native app (Android): no session header, so the nonce and the
// refresh token travel in the body and the session is a native one.

func (h *harness) nativeChallenge(t *testing.T) string {
	t.Helper()
	res := h.call(t, http.MethodPost, "/auth/google/challenge", "", map[string]string{})
	require.Equal(t, http.StatusOK, res.status, res.body)
	return res.data(t)["nonce"].(string)
}

func nativeGoogleBody(nonce, password string) map[string]string {
	return map[string]string{
		"nonce": nonce, "credential": "valid:" + nonce, "password": password,
		"timezone": "America/Lima", "main_currency": "USD",
	}
}

func TestGoogleFromANativeAppKeepsTheNonceAndTheTokensInTheBody(t *testing.T) {
	h, email := googleHarness(t)
	var currency string
	h.module.OnUserRegistered(func(_ context.Context, u identity.NewUser) error {
		currency = u.MainCurrency
		return nil
	})
	raw := h.webCall(t, "/auth/google/challenge", `{}`, nil)
	require.Equal(t, http.StatusOK, raw.Code)

	nonce := h.nativeChallenge(t)
	signedIn := h.call(t, http.MethodPost, "/auth/google", "", nativeGoogleBody(nonce, ""))
	require.Equal(t, http.StatusOK, signedIn.status, signedIn.body)
	session := tokensOf(t, signedIn.data(t))
	assert.NotEmpty(t, session.refresh, "the refresh token comes in the body")
	assert.Equal(t, "Ana Google", signedIn.data(t)["user"].(map[string]any)["name"])

	var web bool
	require.NoError(t, h.pool.QueryRow(context.Background(), `
		SELECT s.is_web FROM sessions s JOIN users u ON u.id = s.user_id WHERE u.email = $1`, email).Scan(&web))
	assert.False(t, web, "a native session, with the native refresh lifetime")
	assert.Equal(t, "USD", currency)

	// It renews like any native session.
	renewed := h.call(t, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": session.refresh})
	assert.Equal(t, http.StatusOK, renewed.status, renewed.body)

	replay := h.call(t, http.MethodPost, "/auth/google", "", nativeGoogleBody(nonce, ""))
	assert.Equal(t, http.StatusUnauthorized, replay.status)
	assert.Equal(t, "GOOGLE_CHALLENGE_EXPIRED", replay.errorCode())
}

func TestANativeChallengeSetsNoCookieAndTheWebIgnoresABodyNonce(t *testing.T) {
	h, _ := googleHarness(t)
	res := h.call(t, http.MethodPost, "/auth/google/challenge", "", map[string]string{})
	require.Equal(t, http.StatusOK, res.status)
	assert.Empty(t, res.header.Values("Set-Cookie"))

	// A browser must prove its nonce with the cookie: one in the body is not enough.
	nonce := h.nativeChallenge(t)
	body, err := json.Marshal(nativeGoogleBody(nonce, ""))
	require.NoError(t, err)
	web := h.webCall(t, "/auth/google", string(body), nil)
	assert.Equal(t, http.StatusUnauthorized, web.Code)
	assert.Contains(t, web.Body.String(), "GOOGLE_CHALLENGE_EXPIRED")
}

func TestANativeAppLinksAnExistingAccountWithItsPassword(t *testing.T) {
	h, email := googleHarness(t)
	h.register(t, email)
	nonce := h.nativeChallenge(t)

	required := h.call(t, http.MethodPost, "/auth/google", "", nativeGoogleBody(nonce, ""))
	assert.Equal(t, http.StatusConflict, required.status)
	assert.Equal(t, "GOOGLE_LINK_REQUIRED", required.errorCode())

	// The same nonce serves the retry with the password.
	linked := h.call(t, http.MethodPost, "/auth/google", "", nativeGoogleBody(nonce, password))
	require.Equal(t, http.StatusOK, linked.status, fmt.Sprint(linked.body))
	assert.NotEmpty(t, tokensOf(t, linked.data(t)).refresh)
}
