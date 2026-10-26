package loans_test

import (
	"bytes"
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
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 10, 24, 17, 0, 0, 0, time.UTC) }

type harness struct {
	router *gin.Engine
	pool   *pgxpool.Pool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	pool := testsupport.MigratedPool(t)
	runner := tx.NewRunner(pool)
	clock := fixedClock{}
	router, api, err := httpserver.NewRouter(httpserver.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), RateLimitPerMinute: 10000})
	require.NoError(t, err)
	accounts, err := identity.New(identity.Deps{Tx: runner, Clock: clock, JWTSecret: strings.Repeat("t", 64), AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: 30 * 24 * time.Hour, PasswordParams: &identity.FastPasswordParams})
	require.NoError(t, err)
	profiles := profile.New(runner, clock)
	accounts.OnUserRegistered(func(ctx context.Context, user identity.NewUser) error {
		return profiles.OnUserRegistered(ctx, user.ID, user.MainCurrency, user.Timezone)
	})
	protected := api.Group("", auth.Require(accounts.Authenticator()))
	accounts.Routes(api, api, protected)
	loans.New(runner, clock, profiles.Reader()).Routes(api, protected)
	return &harness{router, pool}
}

type response struct {
	status int
	data   map[string]any
	raw    map[string]any
}

func (h *harness) call(t *testing.T, method, path, token string, body any) response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	out := response{status: rec.Code}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.raw))
		out.data, _ = out.raw["data"].(map[string]any)
	}
	return out
}

func (h *harness) signUp(t *testing.T) string {
	t.Helper()
	email := testsupport.UniqueEmail("loans")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{"email": email, "password": "loans-tests-123", "timezone": "America/Lima"})
	require.Equal(t, 201, res.status, res.raw)
	return res.data["tokens"].(map[string]any)["access_token"].(string)
}

func createDebtor(t *testing.T, h *harness, token string) string {
	t.Helper()
	r := h.call(t, "POST", "/loan-debtors", token, map[string]any{"id": uuid.NewString(), "name": "Gerson", "currency": "PEN"})
	require.Equal(t, 201, r.status, r.raw)
	return r.data["id"].(string)
}
func movement(kind, amount string) map[string]any {
	b := map[string]any{"id": uuid.NewString(), "kind": kind, "amount": amount, "date": "2026-10-01", "note": "Yape", "title": "Compra"}
	if kind == "payment" {
		b["applied_month"] = "2026-10"
	}
	return b
}
func path(id string) string { return "/loan-debtors/" + id }
func movementData(t *testing.T, r response, id string) map[string]any {
	t.Helper()
	require.Contains(t, r.data, "movements", r.raw)
	for _, entry := range r.data["movements"].([]any) {
		m := entry.(map[string]any)
		if m["id"] == id {
			return m
		}
	}
	t.Fatalf("movement not found")
	return nil
}

func TestCRUDIsolationAndSharing(t *testing.T) {
	h := newHarness(t)
	owner := h.signUp(t)
	other := h.signUp(t)
	id := createDebtor(t, h, owner)
	body := movement("loan", "6000.00")
	res := h.call(t, "POST", path(id)+"/movements", owner, body)
	require.Equal(t, 201, res.status, res.raw)
	require.Equal(t, 200, h.call(t, "POST", path(id)+"/movements", owner, body).status)
	body["amount"] = "6001.00"
	require.Equal(t, 409, h.call(t, "POST", path(id)+"/movements", owner, body).status)
	paid := movement("payment", "1000.00")
	paid["applied_month"] = "2026-12"
	res = h.call(t, "POST", path(id)+"/movements", owner, paid)
	require.Equal(t, 201, res.status, res.raw)
	moves := res.data["movements"].([]any)
	require.Len(t, moves, 2)
	assert.Equal(t, "6000.00", movementData(t, res, body["id"].(string))["amount"])
	assert.Equal(t, "2026-12", movementData(t, res, paid["id"].(string))["applied_month"])
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		res = h.call(t, method, path(id), other, map[string]string{"name": "Wrong"})
		assert.Equal(t, 404, res.status, res.raw)
	}
	require.Equal(t, 401, h.call(t, "GET", path(id), "", nil).status)
	res = h.call(t, "GET", "/loan-debtors", owner, nil)
	require.Equal(t, 200, res.status)
	assert.Len(t, res.raw["data"], 1)
	assert.Equal(t, "2026-10-24", res.raw["meta"].(map[string]any)["today"])
	require.Equal(t, 409, h.call(t, "PATCH", path(id), owner, map[string]string{"name": "Gerson", "currency": "USD"}).status)
	res = h.call(t, "PUT", path(id)+"/share", owner, nil)
	require.Equal(t, 200, res.status, res.raw)
	token := res.data["share_token"].(string)
	assert.Len(t, token, 43)
	shared := h.call(t, "GET", "/shared/loan-debtors/"+token, "", nil)
	require.Equal(t, 200, shared.status, shared.raw)
	assert.NotContains(t, shared.data, "share_token")
	assert.NotContains(t, shared.data, "user_id")
	assert.Len(t, shared.data["movements"], 2)
	require.Equal(t, 401, h.call(t, "POST", path(id)+"/movements", "", movement("loan", "100")).status)
	require.Equal(t, 404, h.call(t, "PUT", path(id)+"/share", other, nil).status)
	rotated := h.call(t, "PUT", path(id)+"/share", owner, nil)
	require.Equal(t, 200, rotated.status)
	newToken := rotated.data["share_token"].(string)
	require.NotEqual(t, token, newToken)
	require.Equal(t, 404, h.call(t, "GET", "/shared/loan-debtors/"+token, "", nil).status)
	require.Equal(t, 200, h.call(t, "DELETE", path(id)+"/share", owner, nil).status)
	require.Equal(t, 404, h.call(t, "GET", "/shared/loan-debtors/"+newToken, "", nil).status)
	require.Equal(t, 200, h.call(t, "PATCH", path(id), owner, map[string]string{"name": "Nuevo nombre"}).status)
	require.Equal(t, 204, h.call(t, "DELETE", path(id), owner, nil).status)
	var count int
	require.NoError(t, h.pool.QueryRow(context.Background(), "SELECT count(*) FROM loan_movements WHERE debtor_id=$1", id).Scan(&count))
	assert.Zero(t, count)
}
func TestCorrectionsValidateLedgerAndPlanAtomically(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t)
	id := createDebtor(t, h, token)
	a := movement("loan", "1000")
	require.Equal(t, 201, h.call(t, "POST", path(id)+"/movements", token, a).status)
	p := movement("payment", "700")
	require.Equal(t, 201, h.call(t, "POST", path(id)+"/movements", token, p).status)
	a["amount"] = "600"
	require.Equal(t, 409, h.call(t, "PATCH", path(id)+"/movements/"+a["id"].(string), token, a).status)
	require.Equal(t, 409, h.call(t, "DELETE", path(id)+"/movements/"+a["id"].(string), token, nil).status)
	p["date"] = "2026-09-30"
	require.Equal(t, 409, h.call(t, "PATCH", path(id)+"/movements/"+p["id"].(string), token, p).status)
	planPath := path(id) + "/plans"
	valid := map[string]any{"plans": []any{map[string]string{"month": "2026-10", "amount": "800"}}}
	require.Equal(t, 200, h.call(t, "PUT", planPath, token, valid).status)
	invalid := map[string]any{"plans": []any{map[string]string{"month": "2026-13", "amount": "800"}}}
	require.Equal(t, 400, h.call(t, "PUT", planPath, token, invalid).status)
	fetched := h.call(t, "GET", path(id), token, nil)
	require.Equal(t, "800.00", fetched.data["plans"].([]any)[0].(map[string]any)["amount"])
	require.Equal(t, 200, h.call(t, "DELETE", path(id)+"/movements/"+p["id"].(string), token, nil).status)
	a["amount"] = "1000"
	a["kind"] = "payment"
	a["applied_month"] = "2026-10"
	require.Equal(t, 409, h.call(t, "PATCH", path(id)+"/movements/"+a["id"].(string), token, a).status)
}
func TestConcurrentPaymentsCannotOverdrawAndRetriesDoNotDuplicate(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t)
	id := createDebtor(t, h, token)
	require.Equal(t, 201, h.call(t, "POST", path(id)+"/movements", token, movement("loan", "1000")).status)
	statuses := make(chan int, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			r := h.call(t, "POST", path(id)+"/movements", token, movement("payment", "700"))
			statuses <- r.status
		}()
	}
	close(start)
	group.Wait()
	close(statuses)
	var ok, conflict int
	for status := range statuses {
		switch status {
		case 201:
			ok++
		case 409:
			conflict++
		}
	}
	assert.Equal(t, 1, ok)
	assert.Equal(t, 1, conflict)
	payment := movement("payment", "100")
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			r := h.call(t, "POST", path(id)+"/movements", token, payment)
			assert.Contains(t, []int{200, 201}, r.status, r.raw)
		}()
	}
	group.Wait()
	res := h.call(t, "GET", path(id), token, nil)
	assert.Len(t, res.data["movements"], 3)
}
func TestValidationOwnerTimeAndMoneyWireFormat(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t)
	id := createDebtor(t, h, token)
	for _, changes := range []map[string]any{{"amount": 100}, {"amount": "1e3"}, {"amount": "-1"}, {"amount": "1.001"}, {"amount": "0"}, {"date": "2026-10-25"}, {"date": "2026-02-30"}, {"currency": "USD"}, {"kind": "other"}} {
		b := movement("loan", "100")
		for k, v := range changes {
			b[k] = v
		}
		r := h.call(t, "POST", path(id)+"/movements", token, b)
		assert.Equal(t, 400, r.status, r.raw)
	}
	b := movement("payment", "1")
	delete(b, "applied_month")
	require.Equal(t, 400, h.call(t, "POST", path(id)+"/movements", token, b).status)
	foreign := createDebtor(t, h, token)
	a := movement("loan", "100")
	require.Equal(t, 201, h.call(t, "POST", path(id)+"/movements", token, a).status)
	require.Equal(t, 409, h.call(t, "POST", path(foreign)+"/movements", token, a).status)
	require.Equal(t, 404, h.call(t, "GET", "/shared/loan-debtors/not-a-token", "", nil).status)
	// At 17:00 UTC it is still October 24 in Lima, even if another profile's local day differs.
	require.Equal(t, 201, h.call(t, "POST", path(id)+"/movements", token, movement("loan", "0.01")).status)
}
