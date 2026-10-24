package services_test

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
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services"
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
	services.New(runner, clock, profiles.Reader()).Routes(protected)
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
	email := testsupport.UniqueEmail("services")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{"email": email, "password": "service-tests-123", "timezone": "America/Lima"})
	require.Equal(t, 201, res.status, res.raw)
	return res.data["tokens"].(map[string]any)["access_token"].(string)
}

func draft() map[string]any {
	return map[string]any{"id": uuid.NewString(), "name": "Netflix", "color": "#1D4ED8", "currency": "PEN", "amount": "30.00", "billing_day": 10, "started_on": "2026-01-01", "status": "active", "ended_on": nil, "note": "Manual shares", "shared": true, "members": []any{map[string]any{"id": uuid.NewString(), "name": "Ana", "amount": "10.00", "since": "2026-01"}}}
}

func create(t *testing.T, h *harness, token string, body map[string]any) string {
	t.Helper()
	res := h.call(t, http.MethodPost, "/services", token, body)
	require.Equal(t, 201, res.status, res.raw)
	return res.data["id"].(string)
}

func payment(member, month string) map[string]any {
	return map[string]any{"member_id": member, "month": month, "paid_on": "2026-10-20", "amount": "0.01"}
}

func memberID(body map[string]any) string {
	return body["members"].([]any)[0].(map[string]any)["id"].(string)
}

func TestServicesCRUDAndOwnerIsolation(t *testing.T) {
	h := newHarness(t)
	owner := h.signUp(t)
	other := h.signUp(t)
	body := draft()
	id := create(t, h, owner, body)
	again := h.call(t, http.MethodPost, "/services", owner, body)
	require.Equal(t, 200, again.status)
	fetched := h.call(t, http.MethodGet, "/services/"+id, owner, nil)
	require.Equal(t, 200, fetched.status)
	assert.Equal(t, "30.00", fetched.data["amount"])
	assert.NotContains(t, fetched.data, "category")
	list := h.call(t, http.MethodGet, "/services", owner, nil)
	require.Equal(t, 200, list.status)
	assert.Len(t, list.raw["data"], 1)
	assert.Equal(t, "2026-10-24", list.raw["meta"].(map[string]any)["today"])
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		r := h.call(t, method, "/services/"+id, other, body)
		assert.Equal(t, 404, r.status, r.raw)
	}
	foreignCreate := h.call(t, http.MethodPost, "/services", other, body)
	assert.Equal(t, 409, foreignCreate.status)
	body["name"] = "Netflix Premium"
	body["amount"] = "40.00"
	updated := h.call(t, http.MethodPatch, "/services/"+id, owner, body)
	assert.Equal(t, 200, updated.status, updated.raw)
	assert.Equal(t, "Netflix Premium", h.call(t, http.MethodGet, "/services/"+id, owner, nil).data["name"])
	assert.Equal(t, 204, h.call(t, http.MethodDelete, "/services/"+id, owner, nil).status)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/services/"+id, owner, nil).status)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/services", "", nil).status)
}

func TestPaymentsAreAtomicIdempotentAndProtectCurrency(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t)
	body := draft()
	id := create(t, h, token, body)
	member := memberID(body)
	batch := map[string]any{"payments": []any{payment(member, "2026-08"), payment(member, "2026-13")}}
	bad := h.call(t, http.MethodPut, "/services/"+id+"/payments", token, batch)
	assert.Equal(t, 400, bad.status)
	assert.Empty(t, h.call(t, http.MethodGet, "/services/"+id, token, nil).data["member_payments"])
	batch["payments"] = []any{payment(member, "2026-08"), payment(member, "2026-09")}
	for range 2 {
		res := h.call(t, http.MethodPut, "/services/"+id+"/payments", token, batch)
		require.Equal(t, 200, res.status, res.raw)
		require.Len(t, res.data["member_payments"], 2)
		assert.Equal(t, "10.00", res.data["member_payments"].([]any)[0].(map[string]any)["amount"])
	}
	body["currency"] = "USD"
	locked := h.call(t, http.MethodPatch, "/services/"+id, token, body)
	assert.Equal(t, 409, locked.status)
	body["currency"] = "PEN"
	body["members"].([]any)[0].(map[string]any)["amount"] = "12.00"
	require.Equal(t, 200, h.call(t, http.MethodPatch, "/services/"+id, token, body).status)
	kept := h.call(t, http.MethodGet, "/services/"+id, token, nil)
	assert.Equal(t, "10.00", kept.data["member_payments"].([]any)[0].(map[string]any)["amount"])
	unmarked := h.call(t, http.MethodDelete, "/services/"+id+"/members/"+member+"/payments/2026-08", token, nil)
	require.Equal(t, 200, unmarked.status)
	assert.Len(t, unmarked.data["member_payments"], 1)
	body["shared"] = false
	res := h.call(t, http.MethodPatch, "/services/"+id, token, body)
	require.Equal(t, 200, res.status, res.raw)
	assert.Empty(t, res.data["members"])
	assert.Empty(t, res.data["member_payments"])
}

func TestForeignMembersCannotBeAttachedOrPaid(t *testing.T) {
	h := newHarness(t)
	owner := h.signUp(t)
	other := h.signUp(t)
	a := draft()
	id := create(t, h, owner, a)
	b := draft()
	b["members"] = a["members"]
	rejected := h.call(t, http.MethodPost, "/services", other, b)
	assert.Equal(t, 409, rejected.status, rejected.raw)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/services/"+b["id"].(string), other, nil).status)
	forged := map[string]any{"payments": []any{payment(memberID(a), "2026-09")}}
	assert.Equal(t, 404, h.call(t, http.MethodPut, "/services/"+id+"/payments", other, forged).status)
	c := draft()
	ownID := create(t, h, other, c)
	assert.Equal(t, 404, h.call(t, http.MethodPut, "/services/"+ownID+"/payments", other, forged).status)
}

func TestConcurrentPaymentsHaveOneRecord(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t)
	body := draft()
	id := create(t, h, token, body)
	batch := map[string]any{"payments": []any{payment(memberID(body), "2026-09")}}
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Go(func() { statuses[i] = h.call(t, http.MethodPut, "/services/"+id+"/payments", token, batch).status })
	}
	wg.Wait()
	assert.Equal(t, []int{200, 200}, statuses)
	assert.Len(t, h.call(t, http.MethodGet, "/services/"+id, token, nil).data["member_payments"], 1)
}

func TestInvalidSharesDatesAndFuturePaymentsAreRejected(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t)
	for _, mutate := range []func(map[string]any){
		func(b map[string]any) { b["amount"] = "9.99" }, func(b map[string]any) { b["started_on"] = "2026-02-30" }, func(b map[string]any) { b["billing_day"] = 32 }, func(b map[string]any) { b["currency"] = "EUR" }, func(b map[string]any) { b["status"] = "cancelled" },
	} {
		body := draft()
		mutate(body)
		assert.Equal(t, 400, h.call(t, http.MethodPost, "/services", token, body).status)
	}
	body := draft()
	id := create(t, h, token, body)
	p := payment(memberID(body), "2026-11")
	assert.Equal(t, 400, h.call(t, http.MethodPut, "/services/"+id+"/payments", token, map[string]any{"payments": []any{p}}).status)
	p = payment(memberID(body), "2026-09")
	p["paid_on"] = "2026-10-25"
	assert.Equal(t, 400, h.call(t, http.MethodPut, "/services/"+id+"/payments", token, map[string]any{"payments": []any{p}}).status)
}

func TestDeletingAnAccountCascadesOnlyItsServices(t *testing.T) {
	h := newHarness(t)
	owner := h.signUp(t)
	other := h.signUp(t)
	body := draft()
	id := create(t, h, owner, body)
	otherID := create(t, h, other, draft())
	res := h.call(t, http.MethodPut, "/services/"+id+"/payments", owner, map[string]any{"payments": []any{payment(memberID(body), "2026-09")}})
	require.Equal(t, 200, res.status)
	deleted := h.call(t, http.MethodDelete, "/me", owner, map[string]string{"password": "service-tests-123"})
	require.Equal(t, 204, deleted.status, deleted.raw)
	for _, table := range []string{"services", "service_members", "service_member_payments"} {
		var count int
		column := "service_id"
		if table == "services" {
			column = "id"
		}
		require.NoError(t, h.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE "+column+"=$1", id).Scan(&count))
		assert.Zero(t, count)
	}
	assert.Equal(t, 200, h.call(t, http.MethodGet, "/services/"+otherID, other, nil).status)
}
