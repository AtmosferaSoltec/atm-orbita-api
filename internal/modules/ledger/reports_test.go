package ledger_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
)

// Integración del módulo reports (plan 07): sus sumas salen de ledger.

// seedSample carga los movimientos de septiembre y octubre de SampleData.kt y
// la transferencia de dólares, y devuelve los ids por nombre.
func (h *harness) seedSample(t *testing.T, token string) map[string]string {
	t.Helper()
	ids := h.sampleAccounts(t, token)
	post := func(path string, body map[string]any) {
		res := h.call(t, http.MethodPost, path, token, body)
		require.Equal(t, http.StatusCreated, res.status, res.body)
	}
	for i := len(september) - 1; i >= 0; i-- {
		m := september[i]
		post("/transactions", map[string]any{
			"account_id": ids[m.account], "category_id": ids[m.category], "amount": m.amount,
			"description": m.text, "occurred_on": m.date,
		})
	}
	post("/transactions", map[string]any{"account_id": ids["Débito principal"], "category_id": ids["Sueldo"], "amount": "3500.00", "description": "Sueldo", "occurred_on": "2026-10-01"})
	post("/transfers", map[string]any{
		"from_account_id": ids["Cuenta Dólares"], "to_account_id": ids["Débito principal"], "from_amount": "20.00",
		"to_amount": "64.00", "exchange_rate": "3.20", "occurred_on": "2026-10-01", "note": "Cambio de dólares",
	})
	post("/transactions", map[string]any{"account_id": ids["Débito principal"], "category_id": ids["Transporte"], "amount": "12.00", "description": "Taxi al trabajo", "occurred_on": "2026-10-02"})
	post("/transactions", map[string]any{"account_id": ids["Efectivo"], "category_id": ids["Alimentación"], "amount": "18.50", "description": "Almuerzo", "occurred_on": "2026-10-02"})
	return ids
}

func (h *harness) report(t *testing.T, token, from, to string) map[string]any {
	t.Helper()
	res := h.call(t, http.MethodGet, "/reports/summary?from="+from+"&to="+to, token, nil)
	require.Equal(t, http.StatusOK, res.status, res.body)
	return res.data()
}

func block(report map[string]any, i int) map[string]any {
	return report["currencies"].([]any)[i].(map[string]any)
}

func ranking(block map[string]any, kind string) [][2]string {
	var out [][2]string
	for _, item := range block[kind].([]any) {
		line := item.(map[string]any)
		out = append(out, [2]string{line["category"].(map[string]any)["name"].(string), line["total"].(string) + " " + line["percent"].(string)})
	}
	return out
}

// El 2 de octubre a mediodía en Lima, el "hoy" de los datos de ejemplo.
var sampleToday = clock.Fixed{At: time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)}

func TestReportsOfTheSample(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "reports", map[string]string{"timezone": "America/Lima"})
	h.seedSample(t, token)

	sep := h.report(t, token, "2026-09-01", "2026-09-30")
	require.Len(t, sep["currencies"], 1)
	pen := block(sep, 0)
	assert.Equal(t, "PEN", pen["currency"])
	assert.Equal(t, "4200.00", pen["income_total"])
	assert.Equal(t, "2148.60", pen["expense_total"])
	assert.Equal(t, "2051.40", pen["balance"])
	assert.Equal(t, [][2]string{{"Sueldo", "3500.00 83.3"}, {"Freelance", "600.00 14.3"}, {"Otros ingresos", "100.00 2.4"}}, ranking(pen, "incomes"))
	assert.Equal(t, [][2]string{
		{"Vivienda", "800.00 37.2"}, {"Alimentación", "612.40 28.5"}, {"Transporte", "215.20 10.0"},
		{"Ocio", "190.00 8.8"}, {"Otros", "183.00 8.5"}, {"Salud", "148.00 6.9"},
	}, ranking(pen, "expenses"))
	assert.Equal(t, "expense", pen["expenses"].([]any)[0].(map[string]any)["category"].(map[string]any)["kind"])

	// Octubre se recorta a hoy; la transferencia de US$ 20 no es ingreso ni gasto.
	oct := h.report(t, token, "2026-10-01", "2026-10-31")
	assert.Equal(t, "2026-10-02", oct["to"])
	require.Len(t, oct["currencies"], 1)
	assert.Equal(t, "3500.00", block(oct, 0)["income_total"])
	assert.Equal(t, "30.50", block(oct, 0)["expense_total"])

	year := h.report(t, token, "2026-01-01", "2026-12-31")
	assert.Equal(t, "7700.00", block(year, 0)["income_total"])
	assert.Equal(t, "2179.10", block(year, 0)["expense_total"])

	assert.Empty(t, h.report(t, token, "2026-08-01", "2026-08-31")["currencies"])
	assert.Empty(t, h.report(t, token, "2026-11-01", "2026-11-30")["currencies"], "un periodo en el futuro no tiene datos")
	res := h.call(t, http.MethodGet, "/reports/summary?from=2026-10-31&to=2026-10-01", token, nil)
	assert.Equal(t, []string{"RANGE_INVALID"}, res.fieldCodes())
}

func TestADollarMovementGetsItsOwnBlockAndDeletedOnesDoNotCount(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "reportusd", map[string]string{"timezone": "America/Lima"})
	ids := h.sampleAccounts(t, token)

	res := h.call(t, http.MethodPost, "/transactions", token, map[string]any{
		"account_id": ids["Cuenta Dólares"], "category_id": ids["Ocio"], "amount": "12.00", "occurred_on": "2026-10-01",
	})
	require.Equal(t, http.StatusCreated, res.status)
	res = h.call(t, http.MethodPost, "/transactions", token, map[string]any{
		"account_id": ids["Efectivo"], "category_id": ids["Alimentación"], "amount": "18.50", "occurred_on": "2026-10-02",
	})
	require.Equal(t, http.StatusCreated, res.status)
	gone := res.data()["id"].(string)
	res = h.call(t, http.MethodPost, "/transactions", token, map[string]any{
		"account_id": ids["Efectivo"], "category_id": ids["Sueldo"], "amount": "100.00", "occurred_on": "2026-10-02",
	})
	require.Equal(t, http.StatusCreated, res.status)

	oct := h.report(t, token, "2026-10-01", "2026-10-31")
	require.Len(t, oct["currencies"], 2)
	assert.Equal(t, "PEN", block(oct, 0)["currency"])
	assert.Equal(t, "USD", block(oct, 1)["currency"])
	assert.Equal(t, "12.00", block(oct, 1)["expense_total"])
	assert.Equal(t, []any{}, block(oct, 1)["incomes"])

	h.call(t, http.MethodDelete, "/transactions/"+gone, token, nil)
	oct = h.report(t, token, "2026-10-01", "2026-10-31")
	assert.Equal(t, "0.00", block(oct, 0)["expense_total"])
	assert.Equal(t, []any{}, block(oct, 0)["expenses"])

	other := h.signUp(t, "reportother")
	assert.Empty(t, h.report(t, other, "2026-10-01", "2026-10-31")["currencies"])
}
