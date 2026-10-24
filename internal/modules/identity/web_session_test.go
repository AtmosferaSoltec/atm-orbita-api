package identity_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
)

// Plan 17: la web guarda el refresh token en una cookie HttpOnly y la cuenta
// puede tener nombre.

func (h *harness) webCall(t *testing.T, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Transport", "cookie")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func refreshCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "orbita_refresh" {
			return c
		}
	}
	t.Fatalf("no orbita_refresh cookie in %v", rec.Header())
	return nil
}

func TestRegisterWithNameShowsItInMe(t *testing.T) {
	h := newHarness(t, 1000)
	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": testsupport.UniqueEmail("websession"), "password": password, "name": "  Joel   Maldonado ",
	})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	assert.Equal(t, "Joel Maldonado", res.data(t)["user"].(map[string]any)["name"])

	me := h.call(t, http.MethodGet, "/me", tokensOf(t, res.data(t)).access, nil)
	assert.Equal(t, "Joel Maldonado", me.data(t)["name"])
}

func TestRegisterWithoutNameKeepsWorkingForAndroid(t *testing.T) {
	h := newHarness(t, 1000)
	access := h.register(t, testsupport.UniqueEmail("websession")).access
	me := h.call(t, http.MethodGet, "/me", access, nil)
	name, present := me.data(t)["name"]
	assert.True(t, present)
	assert.Nil(t, name)
}

func TestRegisterRejectsLongName(t *testing.T) {
	h := newHarness(t, 1000)
	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": testsupport.UniqueEmail("websession"), "password": password, "name": strings.Repeat("a", 61),
	})
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Contains(t, res.body["error"].(map[string]any)["details"], map[string]any{"field": "name", "code": "NAME_TOO_LONG"})
}

func TestWebSessionUsesHttpOnlyCookie(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("websession")
	h.register(t, email)

	login := h.webCall(t, "/auth/login", `{"email":"`+email+`","password":"`+password+`"}`, nil)
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())
	assert.NotContains(t, login.Body.String(), "refresh_token", "the token must not reach JavaScript")
	assert.Contains(t, login.Body.String(), "access_token")
	first := refreshCookie(t, login)
	assert.True(t, first.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, first.SameSite)
	assert.Equal(t, "/api/v1/auth", first.Path)
	assert.NotEmpty(t, first.Value)

	// Renovar lee la cookie (cuerpo vacío) y la rota.
	refreshed := h.webCall(t, "/auth/refresh", "", first)
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	assert.NotContains(t, refreshed.Body.String(), "refresh_token")
	second := refreshCookie(t, refreshed)
	assert.NotEqual(t, first.Value, second.Value)

	// Reusar la cookie vieja revoca la sesión entera y borra la cookie.
	reused := h.webCall(t, "/auth/refresh", "", first)
	assert.Equal(t, http.StatusUnauthorized, reused.Code)
	assert.Equal(t, -1, refreshCookie(t, reused).MaxAge)
	assert.Equal(t, http.StatusUnauthorized, h.webCall(t, "/auth/refresh", "", second).Code)
}

func TestWebLogoutClearsCookie(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("websession")
	h.register(t, email)
	login := h.webCall(t, "/auth/login", `{"email":"`+email+`","password":"`+password+`"}`, nil)
	cookie := refreshCookie(t, login)

	logout := h.webCall(t, "/auth/logout", "", cookie)
	assert.Equal(t, http.StatusNoContent, logout.Code)
	assert.Equal(t, -1, refreshCookie(t, logout).MaxAge)
	assert.Equal(t, http.StatusUnauthorized, h.webCall(t, "/auth/refresh", "", cookie).Code)
}

func TestWebRefreshWithoutCookieIsSessionExpired(t *testing.T) {
	h := newHarness(t, 1000)
	rec := h.webCall(t, "/auth/refresh", "", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "SESSION_EXPIRED")
}
