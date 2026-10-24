package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/mail"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/middleware"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// Integración: el módulo entero por HTTP contra el Postgres local (TEST_DATABASE_URL).

const password = "vidanueva123"

type harness struct {
	router *gin.Engine
	pool   *pgxpool.Pool
	module *identity.Module
	mailer *captureMailer
	clock  *movableClock
}

func newHarness(t *testing.T, loginPerMinute int, options ...func(*identity.Deps)) *harness {
	t.Helper()
	pool := testsupport.MigratedPool(t)
	gin.SetMode(gin.TestMode)
	router, api, err := httpserver.NewRouter(httpserver.Options{
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		RateLimitPerMinute: 10_000,
	})
	require.NoError(t, err)
	mailer := &captureMailer{}
	now := &movableClock{at: time.Now()}
	deps := identity.Deps{
		Tx: tx.NewRunner(pool), Clock: now, JWTSecret: strings.Repeat("k", 64),
		AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: 30 * 24 * time.Hour, PasswordParams: &identity.FastPasswordParams,
		Mailer: mailer, Background: func(work func()) { work() },
	}
	for _, option := range options {
		option(&deps)
	}
	module, err := identity.New(deps)
	require.NoError(t, err)
	limited := api.Group("", middleware.NewRateLimiter(loginPerMinute).Middleware())
	protected := api.Group("", auth.Require(module.Authenticator()))
	module.Routes(api, limited, protected)
	return &harness{router: router, pool: pool, module: module, mailer: mailer, clock: now}
}

type response struct {
	status int
	header http.Header
	body   map[string]any
}

func (h *harness) call(t *testing.T, method, path, accessToken string, payload any) response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		body = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, "/api/v1"+path, body)
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	out := response{status: rec.Code, header: rec.Header()}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

func (r response) data(t *testing.T) map[string]any {
	t.Helper()
	data, ok := r.body["data"].(map[string]any)
	require.True(t, ok, "no data in %v", r.body)
	return data
}

func (r response) errorCode() string {
	e, _ := r.body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

type tokens struct{ access, refresh string }

func tokensOf(t *testing.T, data map[string]any) tokens {
	t.Helper()
	raw, ok := data["tokens"].(map[string]any)
	require.True(t, ok)
	return tokens{access: raw["access_token"].(string), refresh: raw["refresh_token"].(string)}
}

func (h *harness) register(t *testing.T, email string) tokens {
	t.Helper()
	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{"email": email, "password": password})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	return tokensOf(t, res.data(t))
}

func TestTheWholeSessionLifecycle(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("lifecycle")
	testsupport.DeleteUsersByEmail(t, h.pool, email)

	registered := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": "  " + strings.ToUpper(email) + " ", "password": password, "main_currency": "PEN", "timezone": "America/Lima",
	})
	require.Equal(t, http.StatusCreated, registered.status, registered.body)
	data := registered.data(t)
	assert.Equal(t, email, data["user"].(map[string]any)["email"])
	first := tokensOf(t, data)
	assert.EqualValues(t, 900, data["tokens"].(map[string]any)["access_token_expires_in"])

	me := h.call(t, http.MethodGet, "/me", first.access, nil)
	require.Equal(t, http.StatusOK, me.status)
	assert.Equal(t, email, me.data(t)["email"])

	refreshed := h.call(t, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": first.refresh})
	require.Equal(t, http.StatusOK, refreshed.status, refreshed.body)
	second := tokensOf(t, refreshed.data(t))
	assert.NotEqual(t, first.refresh, second.refresh)

	logout := h.call(t, http.MethodPost, "/auth/logout", "", map[string]string{"refresh_token": second.refresh})
	assert.Equal(t, http.StatusNoContent, logout.status)

	// Cerrar sesión impide renovar y corta el access token vigente al instante.
	again := h.call(t, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": second.refresh})
	assert.Equal(t, http.StatusUnauthorized, again.status)
	assert.Equal(t, "SESSION_EXPIRED", again.errorCode())
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/me", second.access, nil).status)

	// Iniciar sesión de nuevo abre otra sesión.
	login := h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": password})
	require.Equal(t, http.StatusOK, login.status)
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", tokensOf(t, login.data(t)).access, nil).status)
}

func TestTheDatabaseOnlyKeepsHashes(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("hashes")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	tok := h.register(t, email)

	var passwordHash string
	var tokenHashes int
	ctx := context.Background()
	require.NoError(t, h.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email = $1`, email).Scan(&passwordHash))
	assert.True(t, strings.HasPrefix(passwordHash, "$argon2id$"))
	assert.NotContains(t, passwordHash, password)

	require.NoError(t, h.pool.QueryRow(ctx, `
		SELECT count(*) FROM refresh_tokens r JOIN sessions s ON s.id = r.session_id
		JOIN users u ON u.id = s.user_id
		WHERE u.email = $1 AND r.token_hash <> convert_to($2, 'UTF8')`, email, tok.refresh).Scan(&tokenHashes))
	assert.Equal(t, 1, tokenHashes)
}

func TestRegisterTwiceWithAnotherCaseIsAConflict(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("twice")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	h.register(t, email)

	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{"email": strings.ToUpper(email), "password": password})
	assert.Equal(t, http.StatusConflict, res.status)
	assert.Equal(t, "EMAIL_ALREADY_REGISTERED", res.errorCode())
}

func TestValidationErrorsComeInTheOrderOfTheApp(t *testing.T) {
	h := newHarness(t, 1000)
	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{
		"email": "no-es-correo", "password": "corta", "main_currency": "XXX",
	})
	require.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, "VALIDATION_FAILED", res.errorCode())
	details := res.body["error"].(map[string]any)["details"].([]any)
	var codes []string
	for _, d := range details {
		codes = append(codes, d.(map[string]any)["code"].(string))
	}
	assert.Equal(t, []string{"EMAIL_INVALID", "PASSWORD_TOO_SHORT", "CURRENCY_NOT_SUPPORTED"}, codes)

	broken := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, broken)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWrongPasswordAndUnknownEmailLookTheSame(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("same")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	h.register(t, email)

	wrong := h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": "equivocada"})
	unknown := h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": testsupport.UniqueEmail("nobody"), "password": password})
	assert.Equal(t, http.StatusUnauthorized, wrong.status)
	assert.Equal(t, wrong.status, unknown.status)
	assert.Equal(t, wrong.body, unknown.body)
	assert.Equal(t, "INVALID_CREDENTIALS", wrong.errorCode())
}

func TestTwoRefreshesAtOnceWithTheSameToken(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("race")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	tok := h.register(t, email)

	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Go(func() {
			statuses[i] = h.call(t, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": tok.refresh}).status
		})
	}
	wg.Wait()
	assert.ElementsMatch(t, []int{http.StatusOK, http.StatusUnauthorized}, statuses)
}

func TestAFailingRegistrationHookLeavesNothing(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("hook")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	h.module.OnUserRegistered(func(context.Context, identity.NewUser) error { return errors.New("profile failed") })

	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{"email": email, "password": password})
	assert.Equal(t, http.StatusInternalServerError, res.status)

	var count int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email = $1`, email).Scan(&count))
	assert.Zero(t, count)
}

func TestProtectedRoutesNeedAValidToken(t *testing.T) {
	h := newHarness(t, 1000)
	for _, token := range []string{"", "garbage", "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0."} {
		res := h.call(t, http.MethodGet, "/me", token, nil)
		assert.Equal(t, http.StatusUnauthorized, res.status, token)
		assert.Equal(t, "UNAUTHORIZED", res.errorCode())
	}
}

func TestTheSixthLoginInAMinuteIsLimited(t *testing.T) {
	h := newHarness(t, 5)
	for range 5 {
		res := h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": testsupport.UniqueEmail("limit"), "password": password})
		require.Equal(t, http.StatusUnauthorized, res.status)
	}
	res := h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": testsupport.UniqueEmail("limit"), "password": password})
	assert.Equal(t, http.StatusTooManyRequests, res.status)
	assert.NotEmpty(t, res.header.Get("Retry-After"))
}

// captureMailer guarda los correos en lugar de enviarlos.
type captureMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (c *captureMailer) Send(_ context.Context, m mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m)
	return nil
}

func (c *captureMailer) all() []mail.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]mail.Message(nil), c.sent...)
}

// movableClock es el reloj real que una prueba puede adelantar.
type movableClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *movableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *movableClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

var _ clock.Clock = (*movableClock)(nil)
