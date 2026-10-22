package ledger_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleAccounts crea las cuentas de dólares y débito de SampleData.kt con su
// saldo, y el tipo de cambio 3.20.
func (h *harness) sampleAccounts(t *testing.T, token string) map[string]string {
	t.Helper()
	res := h.call(t, http.MethodPut, "/settings/fx", token, map[string]any{"main_currency": "PEN", "secondary_currency": "USD", "rate": "3.20"})
	require.Equal(t, http.StatusOK, res.status)
	for _, a := range []map[string]any{
		{"name": "Cuenta Dólares", "type": "savings", "currency": "USD", "initial_balance": "250.00"},
		{"name": "Débito principal", "type": "debit", "currency": "PEN", "initial_balance": "1245.80"},
	} {
		res := h.call(t, http.MethodPost, "/accounts", token, a)
		require.Equal(t, http.StatusCreated, res.status, res.body)
	}
	return h.ids(t, token)
}

func TestATransferBetweenCurrenciesMovesBothBalances(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "trusd")
	ids := h.sampleAccounts(t, token)

	res := h.call(t, http.MethodPost, "/transfers", token, map[string]any{
		"from_account_id": ids["Cuenta Dólares"], "to_account_id": ids["Débito principal"],
		"from_amount": "20.00", "to_amount": "64.00", "exchange_rate": "3.20",
		"occurred_on": "2026-10-01", "note": "Cambio de dólares",
	})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	transfer := res.data()
	assert.Equal(t, "transfer", transfer["entry_type"])
	assert.Equal(t, "3.200000", transfer["exchange_rate"])
	assert.Equal(t, "USD", transfer["from_account"].(map[string]any)["currency"])
	assert.Equal(t, "Débito principal", transfer["to_account"].(map[string]any)["name"])
	assert.Equal(t, "230.00", h.balanceOf(t, token, "Cuenta Dólares"))
	assert.Equal(t, "1309.80", h.balanceOf(t, token, "Débito principal"))

	// El banco dio 63.40 con 3.17: entran exactamente 63.40.
	id := transfer["id"].(string)
	res = h.call(t, http.MethodPatch, "/transfers/"+id, token, map[string]any{"to_amount": "63.40", "exchange_rate": "3.17"})
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Equal(t, "1309.20", h.balanceOf(t, token, "Débito principal"))

	// No es ingreso ni gasto: no aparece como movimiento en ninguna cuenta.
	list := h.entries(t, token, "from=2026-10-01&to=2026-10-31")
	assert.Equal(t, float64(1), list.body["meta"].(map[string]any)["total"])

	assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/transfers/"+id, token, nil).status)
	assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/transfers/"+id, token, nil).status)
	assert.Equal(t, "250.00", h.balanceOf(t, token, "Cuenta Dólares"))
	assert.Equal(t, "1245.80", h.balanceOf(t, token, "Débito principal"))
}

func TestTransferValidationAndOwnership(t *testing.T) {
	h := newHarness(t)
	alice := h.signUp(t, "tralice")
	bob := h.signUp(t, "trbob")
	ids := h.sampleAccounts(t, alice)
	bobIDs := h.ids(t, bob)

	res := h.call(t, http.MethodPost, "/transfers", alice, map[string]any{
		"from_account_id": ids["Efectivo"], "to_account_id": ids["Efectivo"], "from_amount": "0",
	})
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, []string{"SAME_ACCOUNT", "AMOUNT_REQUIRED", "AMOUNT_REQUIRED"}, res.fieldCodes())

	res = h.call(t, http.MethodPost, "/transfers", alice, map[string]any{
		"from_account_id": ids["Cuenta Dólares"], "to_account_id": ids["Débito principal"], "from_amount": "20.00", "to_amount": "64.00",
	})
	assert.Equal(t, []string{"RATE_REQUIRED"}, res.fieldCodes())

	res = h.call(t, http.MethodPost, "/transfers", alice, map[string]any{
		"from_account_id": ids["Débito principal"], "to_account_id": ids["Efectivo"], "from_amount": "20.00", "to_amount": "19.00",
	})
	assert.Equal(t, []string{"AMOUNTS_MUST_MATCH"}, res.fieldCodes())

	res = h.call(t, http.MethodPost, "/transfers", alice, map[string]any{
		"from_account_id": ids["Débito principal"], "to_account_id": bobIDs["Efectivo"], "from_amount": "20.00", "to_amount": "20.00",
	})
	assert.Equal(t, http.StatusNotFound, res.status)

	res = h.call(t, http.MethodPost, "/transfers", alice, map[string]any{
		"from_account_id": ids["Débito principal"], "to_account_id": ids["Efectivo"], "from_amount": "20.00", "to_amount": "20.00",
	})
	require.Equal(t, http.StatusCreated, res.status)
	id := res.data()["id"].(string)
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodDelete, "/transfers/"+id, bob, nil).status)
	assert.Empty(t, h.entries(t, bob, "from=2000-01-01&to=2009-12-31").body["data"])
}

func TestTheListMixesMovementsAndTransfers(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "trlist")
	ids := h.sampleAccounts(t, token)
	post := func(path string, body map[string]any) {
		res := h.call(t, http.MethodPost, path, token, body)
		require.Equal(t, http.StatusCreated, res.status, res.body)
	}
	// Del más antiguo al más reciente, como SampleData.kt (1 y 2 de octubre).
	post("/transactions", map[string]any{"account_id": ids["Débito principal"], "category_id": ids["Sueldo"], "amount": "3500.00", "description": "Sueldo", "occurred_on": "2026-10-01"})
	post("/transfers", map[string]any{
		"from_account_id": ids["Cuenta Dólares"], "to_account_id": ids["Débito principal"], "from_amount": "20.00",
		"to_amount": "64.00", "exchange_rate": "3.20", "occurred_on": "2026-10-01", "note": "Cambio de dólares",
	})
	post("/transactions", map[string]any{"account_id": ids["Débito principal"], "category_id": ids["Transporte"], "amount": "12.00", "description": "Taxi al trabajo", "occurred_on": "2026-10-02"})
	post("/transactions", map[string]any{"account_id": ids["Efectivo"], "category_id": ids["Alimentación"], "amount": "18.50", "description": "Almuerzo", "occurred_on": "2026-10-02"})

	text := func(res response) []string {
		var out []string
		for _, item := range res.body["data"].([]any) {
			row := item.(map[string]any)
			if row["entry_type"] == "transfer" {
				out = append(out, row["note"].(string))
			} else {
				out = append(out, row["description"].(string))
			}
		}
		return out
	}
	all := h.entries(t, token, "from=2026-10-01&to=2026-10-31")
	assert.Equal(t, []string{"Almuerzo", "Taxi al trabajo", "Cambio de dólares", "Sueldo"}, text(all))
	// Las páginas cortan sobre las dos tablas juntas.
	assert.Equal(t, []string{"Cambio de dólares"}, text(h.entries(t, token, "from=2026-10-01&to=2026-10-31&page=3&page_size=1")))
	assert.Equal(t, []string{"Cambio de dólares"}, text(h.entries(t, token, "from=2026-10-01&to=2026-10-31&q=dólares")))
	assert.Equal(t, []string{"Cambio de dólares"}, text(h.entries(t, token, "from=2026-10-01&to=2026-10-31&account_id="+ids["Cuenta Dólares"])))
	assert.Equal(t, []string{"Taxi al trabajo", "Cambio de dólares", "Sueldo"},
		text(h.entries(t, token, "from=2026-10-01&to=2026-10-31&account_id="+ids["Débito principal"])))
	// 1,245.80 + 3,500.00 − 12.00 + 64.00
	assert.Equal(t, "4797.80", h.balanceOf(t, token, "Débito principal"))
}
