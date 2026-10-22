package profile_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// Integración: identity + profile por HTTP contra el Postgres local, cableados
// como en cmd/api/main.go.

type harness struct {
	router *gin.Engine
	pool   *pgxpool.Pool
	reader profile.Reader
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := testsupport.MigratedPool(t)
	gin.SetMode(gin.TestMode)
	router, api, err := httpserver.NewRouter(httpserver.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), RateLimitPerMinute: 10_000,
	})
	require.NoError(t, err)
	runner := tx.NewRunner(pool)
	identityModule, err := identity.New(identity.Deps{
		Tx: runner, Clock: clock.System{}, JWTSecret: strings.Repeat("k", 64),
		AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: 30 * 24 * time.Hour,
		PasswordParams: &identity.FastPasswordParams,
	})
	require.NoError(t, err)
	profileModule := profile.New(runner, clock.System{})
	identityModule.OnUserRegistered(func(ctx context.Context, u identity.NewUser) error {
		return profileModule.OnUserRegistered(ctx, u.ID, u.MainCurrency, u.Timezone)
	})
	protected := api.Group("", auth.Require(identityModule.Authenticator()))
	identityModule.Routes(api, api, protected)
	profileModule.Routes(api, protected)
	return &harness{router: router, pool: pool, reader: profileModule.Reader()}
}

type response struct {
	status int
	body   map[string]any
	header http.Header
}

func (h *harness) call(t *testing.T, method, path, token string, payload any) response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, "/api/v1"+path, body)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	out := response{status: rec.Code, header: rec.Header()}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

func (r response) data() map[string]any {
	data, _ := r.body["data"].(map[string]any)
	return data
}

func (r response) errorCode() string {
	e, _ := r.body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func (r response) fieldCodes() []string {
	e, _ := r.body["error"].(map[string]any)
	details, _ := e["details"].([]any)
	var codes []string
	for _, d := range details {
		codes = append(codes, d.(map[string]any)["code"].(string))
	}
	return codes
}

// signUp registra y devuelve el access token y el correo.
func (h *harness) signUp(t *testing.T, label string, fields map[string]string) (string, string) {
	t.Helper()
	email := testsupport.UniqueEmail(label)
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	payload := map[string]string{"email": email, "password": "vidanueva123"}
	for k, v := range fields {
		payload[k] = v
	}
	res := h.call(t, http.MethodPost, "/auth/register", "", payload)
	require.Equal(t, http.StatusCreated, res.status, res.body)
	return res.data()["tokens"].(map[string]any)["access_token"].(string), email
}

func TestSigningUpCreatesTheProfileWithoutARate(t *testing.T) {
	h := newHarness(t)
	cases := []struct{ currency, main, secondary string }{
		{"", "PEN", "USD"}, {"MXN", "MXN", "USD"}, {"USD", "USD", "PEN"},
	}
	for _, c := range cases {
		token, _ := h.signUp(t, "profile", map[string]string{"main_currency": c.currency, "timezone": "America/Lima"})
		res := h.call(t, http.MethodGet, "/settings", token, nil)
		require.Equal(t, http.StatusOK, res.status)
		data := res.data()
		assert.Equal(t, c.main, data["main_currency"], c.currency)
		assert.Equal(t, c.secondary, data["secondary_currency"], c.currency)
		assert.Equal(t, c.main, data["display_currency"])
		assert.Equal(t, "America/Lima", data["timezone"])
		assert.Equal(t, false, data["fx_configured"])
		assert.Nil(t, data["rate"])
	}
}

func TestSettingTheRateAndShowingTheSecondary(t *testing.T) {
	h := newHarness(t)
	token, email := h.signUp(t, "fx", nil)

	// Sin tipo de cambio no se ve el ahorro en dólares.
	res := h.call(t, http.MethodPatch, "/settings", token, map[string]string{"display_currency": "USD"})
	assert.Equal(t, http.StatusUnprocessableEntity, res.status)
	assert.Equal(t, "FX_RATE_NOT_CONFIGURED", res.errorCode())

	// Un par sin tipo de cambio, o con cero, no se guarda.
	for _, rate := range []any{nil, "0", ""} {
		res = h.call(t, http.MethodPut, "/settings/fx", token, map[string]any{"main_currency": "PEN", "secondary_currency": "USD", "rate": rate})
		assert.Equal(t, http.StatusBadRequest, res.status)
		assert.Equal(t, []string{"RATE_REQUIRED"}, res.fieldCodes())
	}
	// El dinero viaja como texto: un número JSON no es un tipo de cambio.
	res = h.call(t, http.MethodPut, "/settings/fx", token, map[string]any{"main_currency": "PEN", "secondary_currency": "USD", "rate": 3.2})
	assert.Equal(t, http.StatusBadRequest, res.status)

	res = h.call(t, http.MethodPut, "/settings/fx", token, map[string]any{"main_currency": "PEN", "secondary_currency": "USD", "rate": "3.20"})
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Equal(t, "3.200000", res.data()["rate"])
	assert.Equal(t, true, res.data()["fx_configured"])
	assert.NotNil(t, res.data()["rate_updated_at"])

	res = h.call(t, http.MethodPatch, "/settings", token, map[string]string{"display_currency": "USD"})
	require.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "USD", res.data()["display_currency"])

	// Cada cambio es una fila nueva: el historial se conserva.
	h.call(t, http.MethodPut, "/settings/fx", token, map[string]any{"main_currency": "PEN", "secondary_currency": "USD", "rate": "3.75"})
	var rows int
	require.NoError(t, h.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM exchange_rates r JOIN users u ON u.id = r.user_id
		WHERE u.email = $1`, email).Scan(&rows))
	assert.Equal(t, 2, rows)
}

func TestSwappingThePairKeepsTheInverse(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp(t, "swap", nil)
	h.call(t, http.MethodPut, "/settings/fx", token, map[string]any{"main_currency": "PEN", "secondary_currency": "USD", "rate": "3.20"})
	res := h.call(t, http.MethodPut, "/settings/fx", token, map[string]any{"main_currency": "USD", "secondary_currency": "PEN", "rate": "0.3125"})
	require.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "USD", res.data()["main_currency"])
	assert.Equal(t, "0.312500", res.data()["rate"])
}

func TestTheCurrencyCatalogIsPublic(t *testing.T) {
	h := newHarness(t)
	res := h.call(t, http.MethodGet, "/currencies", "", nil)
	require.Equal(t, http.StatusOK, res.status)
	list := res.body["data"].([]any)
	assert.Len(t, list, 9)
	assert.Equal(t, map[string]any{"code": "PEN", "symbol": "S/", "name": "Soles"}, list[0])
	assert.Contains(t, res.header.Get("Cache-Control"), "max-age=86400")
}

func TestSettingsNeedASession(t *testing.T) {
	h := newHarness(t)
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/settings", "", nil).status)
}

func TestEachUserSeesOnlyTheirOwnRate(t *testing.T) {
	h := newHarness(t)
	ana, _ := h.signUp(t, "ana", nil)
	beto, _ := h.signUp(t, "beto", nil)
	h.call(t, http.MethodPut, "/settings/fx", ana, map[string]any{"main_currency": "PEN", "secondary_currency": "USD", "rate": "3.20"})

	res := h.call(t, http.MethodGet, "/settings", beto, nil)
	assert.Equal(t, false, res.data()["fx_configured"])
}

func TestTheReaderGivesTodayInTheUsersTimeZone(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp(t, "today", map[string]string{"timezone": "Pacific/Kiritimati"})
	res := h.call(t, http.MethodGet, "/me", token, nil)
	require.Equal(t, http.StatusOK, res.status)
	userID := res.data()["id"].(string)

	today, err := h.reader.Today(context.Background(), uuid.MustParse(userID))
	require.NoError(t, err)
	// Kiritimati (UTC+14) va un día por delante de casi todo el mundo.
	want := time.Now().UTC().Add(14 * time.Hour).Format("2006-01-02")
	assert.Equal(t, want, today.String())
}
