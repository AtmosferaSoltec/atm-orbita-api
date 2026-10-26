package identity_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
)

// Plan 21: quien entró con Google no tiene contraseña; elimina su cuenta
// confirmando con Google, sin sesión (Google Play lo pide también desde la web).

func (h *harness) rowsOfUser(t *testing.T, email string) (users, identities int) {
	t.Helper()
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email=$1`, email).Scan(&users))
	require.NoError(t, h.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM external_identities e JOIN users u ON u.id = e.user_id WHERE u.email=$1`, email).Scan(&identities))
	return users, identities
}

// webDeleteWithGoogle is the browser's request: the challenge travels in its cookie.
func (h *harness) webDeleteWithGoogle(t *testing.T, cookie *http.Cookie, credential string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/google/delete-account",
		strings.NewReader(`{"credential":"`+credential+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Transport", "cookie")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// registerFresh registers a password account that is removed when the test ends.
func (h *harness) registerFresh(t *testing.T, label string) tokens {
	t.Helper()
	email := testsupport.UniqueEmail(label)
	t.Cleanup(func() { testsupport.DeleteUsersByEmail(t, h.pool, email) })
	return h.register(t, email)
}

// accessTokenOf reads the access token of a web response (the refresh token is not in its body).
func accessTokenOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Data struct {
			Tokens struct {
				Access string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.NotEmpty(t, payload.Data.Tokens.Access)
	return payload.Data.Tokens.Access
}

func (h *harness) googleAccessToken(t *testing.T) string {
	t.Helper()
	cookie, credential := h.challenge(t)
	res := h.webCall(t, "/auth/google", googleBody(credential, ""), cookie)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	return accessTokenOf(t, res)
}

func TestAGoogleAccountWithoutPasswordCanDeleteItselfFromTheBrowser(t *testing.T) {
	h, email := googleHarness(t)
	access := h.googleAccessToken(t)

	// The password path can never work for this account.
	byPassword := h.call(t, http.MethodDelete, "/me", access, map[string]string{"password": "whatever123"})
	assert.Equal(t, http.StatusUnauthorized, byPassword.status)
	users, _ := h.rowsOfUser(t, email)
	require.Equal(t, 1, users)

	// No session is sent: the visitor may have uninstalled the app and never signed in on the web.
	cookie, credential := h.challenge(t)
	res := h.webDeleteWithGoogle(t, cookie, credential)
	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	assert.Equal(t, -1, refreshCookie(t, res).MaxAge, "any session cookie of the browser is cleared")

	users, identities := h.rowsOfUser(t, email)
	assert.Zero(t, users)
	assert.Zero(t, identities, "the linked Google identity goes with the account")
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/me", access, nil).status)
}

func TestTheGoogleDeletionChallengeIsSingleUse(t *testing.T) {
	h, email := googleHarness(t)
	h.googleAccessToken(t)
	cookie, credential := h.challenge(t)
	require.Equal(t, http.StatusNoContent, h.webDeleteWithGoogle(t, cookie, credential).Code)
	users, _ := h.rowsOfUser(t, email)
	assert.Zero(t, users)

	again := h.webDeleteWithGoogle(t, cookie, credential)
	assert.Equal(t, http.StatusUnauthorized, again.Code)
	assert.Contains(t, again.Body.String(), "GOOGLE_CHALLENGE_EXPIRED")
}

func TestAGoogleWithoutAnOrbitaAccountDeletesNothingAndKeepsTheChallenge(t *testing.T) {
	h, _ := googleHarness(t)
	// A password account exists, but Google was never linked to it.
	mine := h.registerFresh(t, "unlinked")

	cookie, credential := h.challenge(t)
	res := h.webDeleteWithGoogle(t, cookie, credential)
	assert.Equal(t, http.StatusNotFound, res.Code)
	assert.Contains(t, res.Body.String(), "GOOGLE_ACCOUNT_NOT_LINKED")
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", mine.access, nil).status, "the password account is intact")

	// The failed attempt rolled back: the same proof can still be used.
	retry := h.webDeleteWithGoogle(t, cookie, credential)
	assert.Contains(t, retry.Body.String(), "GOOGLE_ACCOUNT_NOT_LINKED", "not GOOGLE_CHALLENGE_EXPIRED")
}

func TestAnAppDeletesWithGoogleSendingTheNonceInTheBody(t *testing.T) {
	h, email := googleHarness(t)
	nonce := h.nativeChallenge(t)
	signedIn := h.call(t, http.MethodPost, "/auth/google", "", nativeGoogleBody(nonce, ""))
	require.Equal(t, http.StatusOK, signedIn.status, signedIn.body)

	nonce = h.nativeChallenge(t)
	res := h.call(t, http.MethodPost, "/auth/google/delete-account", "",
		map[string]string{"nonce": nonce, "credential": "valid:" + nonce})
	require.Equal(t, http.StatusNoContent, res.status, res.body)
	users, _ := h.rowsOfUser(t, email)
	assert.Zero(t, users)
}

func TestGoogleDeletionNeedsItsChallenge(t *testing.T) {
	h, email := googleHarness(t)
	h.googleAccessToken(t)
	_, credential := h.challenge(t)

	noCookie := h.webDeleteWithGoogle(t, nil, credential)
	assert.Equal(t, http.StatusUnauthorized, noCookie.Code, "a browser request without its challenge cookie")
	noCredential := h.webDeleteWithGoogle(t, nil, "")
	assert.Equal(t, http.StatusUnauthorized, noCredential.Code)
	users, _ := h.rowsOfUser(t, email)
	assert.Equal(t, 1, users, "nothing was deleted")
}
