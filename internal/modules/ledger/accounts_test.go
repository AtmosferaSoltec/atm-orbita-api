package ledger_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// accounts devuelve la lista y el ahorro de GET /accounts.
func (h *harness) accounts(t *testing.T, token string) ([]map[string]any, map[string]any) {
	t.Helper()
	res := h.call(t, http.MethodGet, "/accounts", token, nil)
	require.Equal(t, http.StatusOK, res.status, res.body)
	var list []map[string]any
	for _, item := range res.data()["accounts"].([]any) {
		list = append(list, item.(map[string]any))
	}
	return list, res.data()["savings"].(map[string]any)
}

func TestANewUserHasCashWithZeroInTheMainCurrency(t *testing.T) {
	h := newHarness(t)
	token := h.signUpWith(t, "cash", map[string]string{"main_currency": "MXN"})
	list, savings := h.accounts(t, token)
	require.Len(t, list, 1)
	assert.Equal(t, "Efectivo", list[0]["name"])
	assert.Equal(t, "cash", list[0]["type"])
	assert.Equal(t, "MXN", list[0]["currency"])
	assert.Equal(t, "0.00", list[0]["balance"])
	assert.Equal(t, true, list[0]["include_in_savings"])
	assert.Equal(t, []any{map[string]any{"currency": "MXN", "amount": "0.00"}}, savings["totals"])
}

func TestADollarAccountNeedsTheExchangeRate(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "usd")
	usd := map[string]any{"name": "Cuenta Dólares", "type": "savings", "currency": "USD", "initial_balance": "250.00"}

	res := h.call(t, http.MethodPost, "/accounts", token, usd)
	assert.Equal(t, http.StatusUnprocessableEntity, res.status)
	assert.Equal(t, "FX_RATE_NOT_CONFIGURED", res.errorCode())

	res = h.call(t, http.MethodPut, "/settings/fx", token, map[string]any{"main_currency": "PEN", "secondary_currency": "USD", "rate": "3.20"})
	require.Equal(t, http.StatusOK, res.status)
	res = h.call(t, http.MethodPost, "/accounts", token, usd)
	require.Equal(t, http.StatusCreated, res.status, res.body)
	assert.Equal(t, "250.00", res.data()["balance"])

	h.call(t, http.MethodPost, "/accounts", token, map[string]any{"name": "Ahorros", "type": "savings", "currency": "PEN", "initial_balance": "4800"})
	h.call(t, http.MethodPost, "/accounts", token, map[string]any{"name": "Billetera digital", "type": "other", "currency": "PEN", "initial_balance": "85.00", "include_in_savings": false})

	list, savings := h.accounts(t, token)
	assert.Equal(t, []any{"Efectivo", "Cuenta Dólares", "Ahorros", "Billetera digital"}, []any{list[0]["name"], list[1]["name"], list[2]["name"], list[3]["name"]})
	// 0 + 800 + 4800 (la billetera no cuenta) = S/ 5,600.00 = US$ 1,750.00
	assert.Equal(t, []any{
		map[string]any{"currency": "PEN", "amount": "5600.00"},
		map[string]any{"currency": "USD", "amount": "1750.00"},
	}, savings["totals"])
	assert.Equal(t, float64(3), savings["included_count"])
	assert.Equal(t, float64(4), savings["active_count"])
}

func TestAccountValidationAndFixedFields(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "accval")
	res := h.call(t, http.MethodPost, "/accounts", token, map[string]any{"name": " ", "type": "wallet", "currency": "XXX", "initial_balance": "-10.00"})
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, []string{"NAME_REQUIRED", "TYPE_INVALID", "CURRENCY_NOT_SUPPORTED", "AMOUNT_INVALID"}, res.fieldCodes())

	// Un número JSON no es un monto.
	res = h.call(t, http.MethodPost, "/accounts", token, map[string]any{"name": "X", "type": "cash", "currency": "PEN", "initial_balance": 10})
	assert.Equal(t, http.StatusBadRequest, res.status)

	cash, _ := h.accounts(t, token)
	id := cash[0]["id"].(string)
	for _, field := range []string{"currency", "initial_balance"} {
		res = h.call(t, http.MethodPatch, "/accounts/"+id, token, map[string]any{field: "USD"})
		assert.Equal(t, http.StatusBadRequest, res.status, field)
		assert.Equal(t, []string{"FIELD_NOT_EDITABLE"}, res.fieldCodes())
	}

	res = h.call(t, http.MethodPatch, "/accounts/"+id, token, map[string]any{"name": "Caja chica", "include_in_savings": false})
	require.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "Caja chica", res.data()["name"])
	assert.Equal(t, false, res.data()["include_in_savings"])
	assert.Equal(t, "PEN", res.data()["currency"])
}

func TestArchivingAnAccountTakesItOutOfTheListAndTheTotal(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "accarch")
	res := h.call(t, http.MethodPost, "/accounts", token, map[string]any{"name": "Ahorros", "type": "savings", "currency": "PEN", "initial_balance": "100.00"})
	require.Equal(t, http.StatusCreated, res.status)
	id := res.data()["id"].(string)

	res = h.call(t, http.MethodPost, "/accounts/"+id+"/archive", token, nil)
	assert.Equal(t, http.StatusNoContent, res.status)
	list, savings := h.accounts(t, token)
	assert.Len(t, list, 1)
	assert.Equal(t, []any{map[string]any{"currency": "PEN", "amount": "0.00"}}, savings["totals"])
}

func TestTheClientIDMakesCreatingAnAccountIdempotent(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "accidem")
	payload := map[string]any{"id": uuid.NewString(), "name": "Débito", "type": "debit", "currency": "PEN", "initial_balance": "10.50"}
	res := h.call(t, http.MethodPost, "/accounts", token, payload)
	require.Equal(t, http.StatusCreated, res.status)
	res = h.call(t, http.MethodPost, "/accounts", token, payload)
	assert.Equal(t, http.StatusOK, res.status)
	list, _ := h.accounts(t, token)
	assert.Len(t, list, 2)

	payload["name"] = "Otro"
	res = h.call(t, http.MethodPost, "/accounts", token, payload)
	assert.Equal(t, "ID_CONFLICT", res.errorCode())
}

func TestAUserCannotSeeOrTouchAnotherUsersAccounts(t *testing.T) {
	h := newHarness(t)
	alice := h.signUp(t, "accalice")
	bob := h.signUp(t, "accbob")
	aliceAccounts, _ := h.accounts(t, alice)
	aliceCash := aliceAccounts[0]["id"].(string)

	bobAccounts, _ := h.accounts(t, bob)
	assert.NotEqual(t, aliceCash, bobAccounts[0]["id"])
	res := h.call(t, http.MethodPatch, "/accounts/"+aliceCash, bob, map[string]any{"name": "Mía"})
	assert.Equal(t, http.StatusNotFound, res.status)
	res = h.call(t, http.MethodPost, "/accounts/"+aliceCash+"/archive", bob, nil)
	assert.Equal(t, http.StatusNotFound, res.status)
	aliceAccounts, _ = h.accounts(t, alice)
	assert.Equal(t, "Efectivo", aliceAccounts[0]["name"])
}
