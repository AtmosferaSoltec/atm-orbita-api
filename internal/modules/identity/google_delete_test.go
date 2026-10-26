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
// confirmando de nuevo con Google.

func (h *harness) rowsOfUser(t *testing.T, email string) (users, identities int) {
	t.Helper()
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email=$1`, email).Scan(&users))
	require.NoError(t, h.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM external_identities e JOIN users u ON u.id = e.user_id WHERE u.email=$1`, email).Scan(&identities))
	return users, identities
}

func (h *harness) webDeleteWithGoogle(t *testing.T, access string, cookie *http.Cookie, credential string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/google/delete-account",
		strings.NewReader(`{"credential":"`+credential+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Transport", "cookie")
	req.Header.Set("Authorization", "Bearer "+access)
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

	cookie, credential := h.challenge(t)
	res := h.webDeleteWithGoogle(t, access, cookie, credential)
	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	assert.Equal(t, -1, refreshCookie(t, res).MaxAge, "the session cookie is cleared")

	users, identities := h.rowsOfUser(t, email)
	assert.Zero(t, users)
	assert.Zero(t, identities, "the linked Google identity goes with the account")
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/me", access, nil).status)
}

func TestTheGoogleDeletionChallengeIsSingleUse(t *testing.T) {
	h, email := googleHarness(t)
	access := h.googleAccessToken(t)
	cookie, credential := h.challenge(t)
	require.Equal(t, http.StatusNoContent, h.webDeleteWithGoogle(t, access, cookie, credential).Code)
	users, _ := h.rowsOfUser(t, email)
	assert.Zero(t, users)

	// Even if the session were still valid, the same proof cannot be replayed.
	again := h.webDeleteWithGoogle(t, access, cookie, credential)
	assert.NotEqual(t, http.StatusNoContent, again.Code)
}

func TestAnotherGoogleAccountCannotDeleteMine(t *testing.T) {
	h, email := googleHarness(t)
	// A person with a password account tries to delete it with an unrelated Google identity.
	mine := h.registerFresh(t, "other")
	cookie, credential := h.challenge(t)
	res := h.webDeleteWithGoogle(t, mine.access, cookie, credential)
	assert.Equal(t, http.StatusUnauthorized, res.Code)
	assert.Contains(t, res.Body.String(), "INVALID_CREDENTIALS")
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", mine.access, nil).status, "the account is intact")

	// And the Google owner's account is not touched either.
	users, _ := h.rowsOfUser(t, email)
	assert.Zero(t, users, "nobody signed in as the Google owner in this test")
}

func TestAGoogleDeletionFailureGivesTheChallengeBack(t *testing.T) {
	h, _ := googleHarness(t)
	mine := h.registerFresh(t, "retry")
	cookie, credential := h.challenge(t)
	require.Equal(t, http.StatusUnauthorized, h.webDeleteWithGoogle(t, mine.access, cookie, credential).Code)
	// Same nonce again: it is still valid (the failed attempt rolled back).
	second := h.webDeleteWithGoogle(t, mine.access, cookie, credential)
	assert.Contains(t, second.Body.String(), "INVALID_CREDENTIALS", "not GOOGLE_CHALLENGE_EXPIRED")
}

func TestAnAppDeletesWithGoogleSendingTheNonceInTheBody(t *testing.T) {
	h, email := googleHarness(t)
	nonce := h.nativeChallenge(t)
	signedIn := h.call(t, http.MethodPost, "/auth/google", "", nativeGoogleBody(nonce, ""))
	require.Equal(t, http.StatusOK, signedIn.status, signedIn.body)
	access := tokensOf(t, signedIn.data(t)).access

	nonce = h.nativeChallenge(t)
	res := h.call(t, http.MethodPost, "/auth/google/delete-account", access,
		map[string]string{"nonce": nonce, "credential": "valid:" + nonce})
	require.Equal(t, http.StatusNoContent, res.status, res.body)
	users, _ := h.rowsOfUser(t, email)
	assert.Zero(t, users)
}

func TestGoogleDeletionNeedsASessionAndAProof(t *testing.T) {
	h, _ := googleHarness(t)
	cookie, credential := h.challenge(t)
	noSession := h.webDeleteWithGoogle(t, "", cookie, credential)
	assert.Equal(t, http.StatusUnauthorized, noSession.Code)

	access := h.googleAccessToken(t)
	noProof := h.webDeleteWithGoogle(t, access, nil, credential)
	assert.Equal(t, http.StatusUnauthorized, noProof.Code, "a browser request without its challenge cookie")
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", access, nil).status)
}
