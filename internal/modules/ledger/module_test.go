package ledger_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// Integración: identity + profile + ledger por HTTP contra el Postgres local,
// cableados como en cmd/api/main.go.

type harness struct {
	router *gin.Engine
	pool   *pgxpool.Pool
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
	ledgerModule := ledger.New(runner, clock.System{})
	identityModule.OnUserRegistered(func(ctx context.Context, u identity.NewUser) error {
		if err := profileModule.OnUserRegistered(ctx, u.ID, u.MainCurrency, u.Timezone); err != nil {
			return err
		}
		return ledgerModule.OnUserRegistered(ctx, u.ID)
	})
	protected := api.Group("", auth.Require(identityModule.Authenticator()))
	identityModule.Routes(api, api, protected)
	profileModule.Routes(api, protected)
	ledgerModule.Routes(protected)
	return &harness{router: router, pool: pool}
}

type response struct {
	status int
	body   map[string]any
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
	out := response{status: rec.Code}
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

func (h *harness) signUp(t *testing.T, label string) string {
	t.Helper()
	email := testsupport.UniqueEmail(label)
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{"email": email, "password": "vidanueva123"})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	return res.data()["tokens"].(map[string]any)["access_token"].(string)
}

func (h *harness) list(t *testing.T, token string) []map[string]any {
	t.Helper()
	res := h.call(t, http.MethodGet, "/categories", token, nil)
	require.Equal(t, http.StatusOK, res.status)
	var out []map[string]any
	for _, item := range res.body["data"].([]any) {
		out = append(out, item.(map[string]any))
	}
	return out
}

func (h *harness) idOf(t *testing.T, token, name string) string {
	t.Helper()
	for _, c := range h.list(t, token) {
		if c["name"] == name {
			return c["id"].(string)
		}
	}
	t.Fatalf("no category %q", name)
	return ""
}

func TestANewUserHasTheNineInitialCategories(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "cats")
	list := h.list(t, token)
	require.Len(t, list, 9)
	assert.Equal(t, "Alimentación", list[0]["name"])
	assert.Equal(t, "expense", list[0]["kind"])
	assert.Equal(t, "#0E7490", list[0]["color"])
	assert.Equal(t, "Otros ingresos", list[8]["name"])
	assert.Equal(t, "income", list[8]["kind"])
	assert.NotContains(t, list[0], "name_normalized")
}

func TestNamesAreUniquePerKindIgnoringCaseAndAccents(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "unique")

	for _, name := range []string{"alimentacion", "ALIMENTACIÓN", "  Alimentación  "} {
		res := h.call(t, http.MethodPost, "/categories", token, map[string]string{"name": name, "kind": "expense", "color": "#0E7490"})
		assert.Equal(t, http.StatusConflict, res.status, name)
		assert.Equal(t, "CATEGORY_NAME_TAKEN", res.errorCode())
		assert.Equal(t, []string{"NAME_TAKEN"}, res.fieldCodes())
	}

	res := h.call(t, http.MethodPost, "/categories", token, map[string]string{"name": "Alimentación", "kind": "income", "color": "#0E7490"})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	assert.Equal(t, "income", res.data()["kind"])

	// Archivar "Ocio" libera su nombre.
	ocio := h.idOf(t, token, "Ocio")
	res = h.call(t, http.MethodPost, "/categories/"+ocio+"/archive", token, nil)
	assert.Equal(t, http.StatusNoContent, res.status)
	res = h.call(t, http.MethodPost, "/categories", token, map[string]string{"name": "ocio", "kind": "expense", "color": "#BE185D"})
	require.Equal(t, http.StatusCreated, res.status)

	// Renombrar a sí misma con otras mayúsculas no choca; a otra existente, sí.
	newOcio := res.data()["id"].(string)
	res = h.call(t, http.MethodPatch, "/categories/"+newOcio, token, map[string]string{"name": "OCIO"})
	require.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "OCIO", res.data()["name"])
	assert.Equal(t, "expense", res.data()["kind"])
	res = h.call(t, http.MethodPatch, "/categories/"+newOcio, token, map[string]string{"name": "salud"})
	assert.Equal(t, http.StatusConflict, res.status)
}

func TestValidationErrorsComeInTheAppOrder(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "valid")
	res := h.call(t, http.MethodPost, "/categories", token, map[string]string{"name": "", "kind": "expense", "color": "rojo"})
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, []string{"NAME_REQUIRED", "COLOR_INVALID"}, res.fieldCodes())

	res = h.call(t, http.MethodPost, "/categories", token, map[string]string{"name": strings.Repeat("a", 61), "kind": "expense", "color": "#0E7490"})
	assert.Equal(t, []string{"NAME_TOO_LONG"}, res.fieldCodes())

	res = h.call(t, http.MethodPost, "/categories", token, map[string]string{"id": "nope", "name": "X", "kind": "expense", "color": "#0E7490"})
	assert.Equal(t, []string{"INVALID_FORMAT"}, res.fieldCodes())
}

func TestTheClientIDMakesCreatingIdempotent(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "idem")
	id := uuid.NewString()
	payload := map[string]string{"id": id, "name": "Mascotas", "kind": "expense", "color": "#0E7490"}

	res := h.call(t, http.MethodPost, "/categories", token, payload)
	require.Equal(t, http.StatusCreated, res.status)
	assert.Equal(t, id, res.data()["id"])
	res = h.call(t, http.MethodPost, "/categories", token, payload)
	assert.Equal(t, http.StatusOK, res.status)
	assert.Len(t, h.list(t, token), 10)

	payload["color"] = "#64748B"
	res = h.call(t, http.MethodPost, "/categories", token, payload)
	assert.Equal(t, http.StatusConflict, res.status)
	assert.Equal(t, "ID_CONFLICT", res.errorCode())
}

func TestTwoSimultaneousCreatesWithTheSameName(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "race")
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Go(func() {
			res := h.call(t, http.MethodPost, "/categories", token, map[string]string{"name": "Viajes", "kind": "expense", "color": "#0E7490"})
			statuses[i] = res.status
		})
	}
	wg.Wait()
	assert.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, statuses)
}

func TestAUserCannotSeeOrTouchAnotherUsersCategories(t *testing.T) {
	h := newHarness(t)
	alice := h.signUp(t, "alice")
	bob := h.signUp(t, "bob")
	aliceOcio := h.idOf(t, alice, "Ocio")

	for _, c := range h.list(t, bob) {
		assert.NotEqual(t, aliceOcio, c["id"])
	}
	res := h.call(t, http.MethodPatch, "/categories/"+aliceOcio, bob, map[string]string{"name": "Mío"})
	assert.Equal(t, http.StatusNotFound, res.status)
	res = h.call(t, http.MethodPost, "/categories/"+aliceOcio+"/archive", bob, nil)
	assert.Equal(t, http.StatusNotFound, res.status)
	res = h.call(t, http.MethodPost, "/categories/not-a-uuid/archive", bob, nil)
	assert.Equal(t, http.StatusNotFound, res.status)
	assert.Len(t, h.list(t, alice), 9)

	res = h.call(t, http.MethodGet, "/categories", "", nil)
	assert.Equal(t, http.StatusUnauthorized, res.status)
}
