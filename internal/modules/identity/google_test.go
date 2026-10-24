package identity_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
)

type testGoogleVerifier struct{ email string }

func (v testGoogleVerifier) Verify(_ context.Context, credential, nonce string) (identity.GoogleAccount, error) {
	if credential != "valid:"+nonce {
		return identity.GoogleAccount{}, errors.New("unexpected test credential")
	}
	return identity.GoogleAccount{Subject: "google-" + v.email, Email: v.email, Name: "Ana Google"}, nil
}

func googleHarness(t *testing.T) (*harness, string) {
	t.Helper()
	email := testsupport.UniqueEmail("google")
	h := newHarness(t, 1000, func(deps *identity.Deps) {
		deps.GoogleWebClientID = "test.apps.googleusercontent.com"
		deps.GoogleVerifier = testGoogleVerifier{email: email}
	})
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	return h, email
}

func (h *harness) challenge(t *testing.T) (*http.Cookie, string) {
	t.Helper()
	res := h.webCall(t, "/auth/google/challenge", `{}`, nil)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var payload struct {
		Data struct {
			Nonce string `json:"nonce"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &payload))
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == "orbita_google_nonce" {
			assert.True(t, cookie.HttpOnly)
			assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
			return cookie, "valid:" + payload.Data.Nonce
		}
	}
	t.Fatal("missing Google nonce cookie")
	return nil, ""
}

func challengeHash(nonce string) []byte {
	hash := sha256.Sum256([]byte(nonce))
	return hash[:]
}

func googleBody(credential, password string) string {
	return fmt.Sprintf(`{"credential":%q,"password":%q,"timezone":"America/Lima","main_currency":"PEN"}`, credential, password)
}

func TestGoogleCreatesOneAccountAndRejectsReplay(t *testing.T) {
	h, email := googleHarness(t)
	var registrations int
	h.module.OnUserRegistered(func(_ context.Context, u identity.NewUser) error {
		registrations++
		assert.Equal(t, "PEN", u.MainCurrency)
		assert.Equal(t, "America/Lima", u.Timezone)
		return nil
	})
	cookie, credential := h.challenge(t)
	first := h.webCall(t, "/auth/google", googleBody(credential, ""), cookie)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	assert.NotContains(t, first.Body.String(), "refresh_token")
	assert.Equal(t, 90*24*60*60, refreshCookie(t, first).MaxAge)
	assert.Contains(t, first.Body.String(), "Ana Google")
	assert.Equal(t, 1, registrations)
	var noPassword bool
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT password_hash IS NULL FROM users WHERE email=$1`, email).Scan(&noPassword))
	assert.True(t, noPassword)

	replay := h.webCall(t, "/auth/google", googleBody(credential, ""), cookie)
	assert.Equal(t, http.StatusUnauthorized, replay.Code)
	assert.Contains(t, replay.Body.String(), "GOOGLE_CHALLENGE_EXPIRED")

	cookie, credential = h.challenge(t)
	second := h.webCall(t, "/auth/google", googleBody(credential, ""), cookie)
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	assert.Equal(t, 1, registrations)
	var count int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email=$1`, email).Scan(&count))
	assert.Equal(t, 1, count)

	login := h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": password})
	assert.Equal(t, http.StatusUnauthorized, login.status, login.body)
	assert.Equal(t, "INVALID_CREDENTIALS", login.errorCode())
}

func TestGoogleLinkRequiresPasswordAndKeepsUserID(t *testing.T) {
	h, email := googleHarness(t)
	existing := h.register(t, email)
	before := h.call(t, http.MethodGet, "/me", existing.access, nil).data(t)["id"]
	cookie, credential := h.challenge(t)
	required := h.webCall(t, "/auth/google", googleBody(credential, ""), cookie)
	assert.Equal(t, http.StatusConflict, required.Code)
	assert.Contains(t, required.Body.String(), "GOOGLE_LINK_REQUIRED")
	wrong := h.webCall(t, "/auth/google", googleBody(credential, "incorrect"), cookie)
	assert.Equal(t, http.StatusUnauthorized, wrong.Code)

	linked := h.webCall(t, "/auth/google", googleBody(credential, password), cookie)
	require.Equal(t, http.StatusOK, linked.Code, linked.Body.String())
	assert.Contains(t, linked.Body.String(), before.(string))
	cookie, credential = h.challenge(t)
	returning := h.webCall(t, "/auth/google", googleBody(credential, ""), cookie)
	assert.Equal(t, http.StatusOK, returning.Code, returning.Body.String())
}

func TestGoogleExpiredChallengeAndMissingBrowserProof(t *testing.T) {
	h, _ := googleHarness(t)
	cookie, credential := h.challenge(t)
	h.clock.advance(6 * time.Minute)
	expired := h.webCall(t, "/auth/google", googleBody(credential, ""), cookie)
	assert.Equal(t, http.StatusUnauthorized, expired.Code)
	assert.Contains(t, expired.Body.String(), "GOOGLE_CHALLENGE_EXPIRED")
	missing := h.webCall(t, "/auth/google", googleBody(credential, ""), nil)
	assert.Equal(t, http.StatusUnauthorized, missing.Code)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/google", strings.NewReader(googleBody(credential, "")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGoogleHookFailureRollsBackAccountIdentityAndChallenge(t *testing.T) {
	h, email := googleHarness(t)
	h.module.OnUserRegistered(func(context.Context, identity.NewUser) error { return errors.New("profile failed") })
	cookie, credential := h.challenge(t)
	res := h.webCall(t, "/auth/google", googleBody(credential, ""), cookie)
	assert.Equal(t, http.StatusInternalServerError, res.Code)
	var count int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email=$1`, email).Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM google_login_challenges WHERE nonce_hash = $1`, challengeHash(cookie.Value)).Scan(&count))
	assert.Positive(t, count)
}

func TestGoogleProviderIsDisabledWithoutConfiguration(t *testing.T) {
	h := newHarness(t, 1000)
	config := h.call(t, http.MethodGet, "/auth/google/config", "", nil)
	assert.Equal(t, "", config.data(t)["client_id"])
	challenge := h.webCall(t, "/auth/google/challenge", `{}`, nil)
	assert.Equal(t, http.StatusServiceUnavailable, challenge.Code)
}

func TestGoogleCredentialCanOnlyOpenOneConcurrentSession(t *testing.T) {
	h, _ := googleHarness(t)
	cookie, credential := h.challenge(t)
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Go(func() { statuses[i] = h.webCall(t, "/auth/google", googleBody(credential, ""), cookie).Code })
	}
	wg.Wait()
	assert.ElementsMatch(t, []int{http.StatusOK, http.StatusUnauthorized}, statuses)
}

func TestGoogleAccountCanExplicitlySetAPasswordWithAnEmailCode(t *testing.T) {
	h, email := googleHarness(t)
	cookie, credential := h.challenge(t)
	google := h.webCall(t, "/auth/google", googleBody(credential, ""), cookie)
	require.Equal(t, http.StatusOK, google.Code)
	code := h.requestCode(t, email)
	reset := h.reset(t, email, code, password)
	require.Equal(t, http.StatusNoContent, reset.status, reset.body)
	login := h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": password})
	assert.Equal(t, http.StatusOK, login.status, login.body)
	// Setting the password revokes the old Google session too.
	assert.Equal(t, http.StatusUnauthorized, h.webCall(t, "/auth/refresh", "", refreshCookie(t, google)).Code)
	cookie, credential = h.challenge(t)
	assert.Equal(t, http.StatusOK, h.webCall(t, "/auth/google", googleBody(credential, ""), cookie).Code)
}

func TestWebSessionRenewsFor90DaysWhileNativeKeeps30(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("web-ttl")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	native := h.register(t, email)
	web := h.webCall(t, "/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password), nil)
	require.Equal(t, http.StatusOK, web.Code)
	first := refreshCookie(t, web)
	assert.Equal(t, 90*24*60*60, first.MaxAge)
	h.clock.advance(29 * 24 * time.Hour)
	renewed := h.webCall(t, "/auth/refresh", "", first)
	require.Equal(t, http.StatusOK, renewed.Code, renewed.Body.String())
	h.clock.advance(2 * 24 * time.Hour)
	oldNative := h.call(t, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": native.refresh})
	assert.Equal(t, http.StatusUnauthorized, oldNative.status)
	// The session's stored policy cannot be changed by removing the web header.
	refreshed := refreshCookie(t, renewed)
	throughBody := h.call(t, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": refreshed.Value})
	require.Equal(t, http.StatusOK, throughBody.status, throughBody.body)
	h.clock.advance(90 * 24 * time.Hour)
	expired := h.call(t, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": tokensOf(t, throughBody.data(t)).refresh})
	assert.Equal(t, http.StatusUnauthorized, expired.status)
}
