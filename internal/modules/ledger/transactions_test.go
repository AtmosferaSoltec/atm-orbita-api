package ledger_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
)

// ids lee los ids de las categorías y cuentas del usuario por su nombre.
func (h *harness) ids(t *testing.T, token string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, c := range h.list(t, token) {
		out[c["name"].(string)] = c["id"].(string)
	}
	accounts, _ := h.accounts(t, token)
	for _, a := range accounts {
		out[a["name"].(string)] = a["id"].(string)
	}
	return out
}

func (h *harness) balanceOf(t *testing.T, token, name string) string {
	t.Helper()
	accounts, _ := h.accounts(t, token)
	for _, a := range accounts {
		if a["name"] == name {
			return a["balance"].(string)
		}
	}
	t.Fatalf("no account %q", name)
	return ""
}

func (h *harness) entries(t *testing.T, token, query string) response {
	t.Helper()
	res := h.call(t, http.MethodGet, "/entries?"+query, token, nil)
	require.Equal(t, http.StatusOK, res.status, res.body)
	return res
}

func descriptions(res response) []string {
	var out []string
	for _, item := range res.body["data"].([]any) {
		out = append(out, item.(map[string]any)["description"].(string))
	}
	return out
}

func TestAnExpenseMovesTheBalanceExactlyAndDeletingItGivesItBack(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "balance")
	ids := h.ids(t, token)

	res := h.call(t, http.MethodPost, "/transactions", token, map[string]any{
		"account_id": ids["Efectivo"], "category_id": ids["Alimentación"], "amount": "45.52", "description": "Almuerzo",
	})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	entry := res.data()
	assert.Equal(t, "transaction", entry["entry_type"])
	assert.Equal(t, "expense", entry["kind"])
	assert.Equal(t, "45.52", entry["amount"])
	assert.Equal(t, "PEN", entry["currency"])
	assert.Equal(t, "Efectivo", entry["account"].(map[string]any)["name"])
	assert.Equal(t, "Alimentación", entry["category"].(map[string]any)["name"])
	assert.Nil(t, entry["credit_purchase_id"])
	assert.Equal(t, "-45.52", h.balanceOf(t, token, "Efectivo"))

	// Pasarlo a Sueldo lo convierte en ingreso.
	res = h.call(t, http.MethodPatch, "/transactions/"+entry["id"].(string), token, map[string]any{"category_id": ids["Sueldo"]})
	require.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "income", res.data()["kind"])
	assert.Equal(t, "45.52", h.balanceOf(t, token, "Efectivo"))

	res = h.call(t, http.MethodDelete, "/transactions/"+entry["id"].(string), token, nil)
	assert.Equal(t, http.StatusNoContent, res.status)
	res = h.call(t, http.MethodDelete, "/transactions/"+entry["id"].(string), token, nil)
	assert.Equal(t, http.StatusNoContent, res.status)
	assert.Equal(t, "0.00", h.balanceOf(t, token, "Efectivo"))
}

func TestWithoutADateTheMovementTakesTodayInLima(t *testing.T) {
	// 20:00 del 2 de octubre en Lima = 01:00 UTC del 3.
	h := newHarnessAt(t, clock.Fixed{At: time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)})
	token := h.signUpWith(t, "lima", map[string]string{"timezone": "America/Lima"})
	ids := h.ids(t, token)

	res := h.call(t, http.MethodPost, "/transactions", token, map[string]any{
		"account_id": ids["Efectivo"], "category_id": ids["Alimentación"], "amount": "18.50",
	})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	assert.Equal(t, "2026-10-02", res.data()["occurred_on"])

	res = h.call(t, http.MethodPost, "/transactions", token, map[string]any{
		"account_id": ids["Efectivo"], "category_id": ids["Alimentación"], "amount": "18.50", "occurred_on": "2026-10-03",
	})
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, []string{"FUTURE_DATE"}, res.fieldCodes())
}

func TestValidationComesInTheOrderOfTheApp(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "txval")
	res := h.call(t, http.MethodPost, "/transactions", token, map[string]any{"amount": "0", "occurred_on": "2999-01-01"})
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, []string{"AMOUNT_REQUIRED", "ACCOUNT_REQUIRED", "CATEGORY_REQUIRED", "FUTURE_DATE"}, res.fieldCodes())

	res = h.call(t, http.MethodPost, "/transactions", token, map[string]any{"amount": 10})
	assert.Equal(t, http.StatusBadRequest, res.status, "un número JSON no es un monto")
}

func TestAnotherUsersAccountIsNotFoundAndAnArchivedOneIsRefused(t *testing.T) {
	h := newHarness(t)
	alice := h.signUp(t, "txalice")
	bob := h.signUp(t, "txbob")
	aliceIDs, bobIDs := h.ids(t, alice), h.ids(t, bob)

	res := h.call(t, http.MethodPost, "/transactions", alice, map[string]any{
		"account_id": bobIDs["Efectivo"], "category_id": aliceIDs["Sueldo"], "amount": "10.00",
	})
	assert.Equal(t, http.StatusNotFound, res.status)
	res = h.call(t, http.MethodPost, "/transactions", alice, map[string]any{
		"account_id": aliceIDs["Efectivo"], "category_id": bobIDs["Sueldo"], "amount": "10.00",
	})
	assert.Equal(t, http.StatusNotFound, res.status)

	res = h.call(t, http.MethodPost, "/accounts", alice, map[string]any{"name": "Vieja", "type": "cash", "currency": "PEN"})
	require.Equal(t, http.StatusCreated, res.status)
	old := res.data()["id"].(string)
	h.call(t, http.MethodPost, "/accounts/"+old+"/archive", alice, nil)
	res = h.call(t, http.MethodPost, "/transactions", alice, map[string]any{
		"account_id": old, "category_id": aliceIDs["Sueldo"], "amount": "10.00",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, res.status)
	assert.Equal(t, "ACCOUNT_ARCHIVED", res.errorCode())

	// B no ve ni toca los movimientos de A.
	res = h.call(t, http.MethodPost, "/transactions", alice, map[string]any{
		"account_id": aliceIDs["Efectivo"], "category_id": aliceIDs["Sueldo"], "amount": "10.00",
	})
	id := res.data()["id"].(string)
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodPatch, "/transactions/"+id, bob, map[string]any{"amount": "1.00"}).status)
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodDelete, "/transactions/"+id, bob, nil).status)
	assert.Empty(t, h.entries(t, bob, "from=2000-01-01&to=2009-12-31").body["data"])
}

// september son los 16 movimientos de septiembre de SampleData.kt, del más
// reciente al más antiguo (sin la transferencia, plan 06).
var september = []struct{ text, category, account, date, amount string }{
	{"Alquiler", "Vivienda", "Débito principal", "2026-09-30", "800.00"},
	{"Supermercado", "Alimentación", "Débito principal", "2026-09-28", "152.40"},
	{"Diseño de logo", "Freelance", "Débito principal", "2026-09-27", "600.00"},
	{"Cine", "Ocio", "Efectivo", "2026-09-26", "45.00"},
	{"Farmacia", "Salud", "Efectivo", "2026-09-24", "38.00"},
	{"Útiles", "Otros", "Efectivo", "2026-09-20", "60.00"},
	{"Restaurante", "Alimentación", "Efectivo", "2026-09-19", "70.00"},
	{"Gasolina", "Transporte", "Débito principal", "2026-09-17", "120.00"},
	{"Regalo de cumpleaños", "Otros", "Débito principal", "2026-09-15", "123.00"},
	{"Supermercado", "Alimentación", "Débito principal", "2026-09-14", "180.00"},
	{"Concierto", "Ocio", "Débito principal", "2026-09-12", "145.00"},
	{"Venta de libros", "Otros ingresos", "Efectivo", "2026-09-12", "100.00"},
	{"Consulta médica", "Salud", "Débito principal", "2026-09-09", "110.00"},
	{"Pasajes de bus", "Transporte", "Efectivo", "2026-09-08", "95.20"},
	{"Mercado", "Alimentación", "Débito principal", "2026-09-06", "210.00"},
	{"Sueldo", "Sueldo", "Débito principal", "2026-09-01", "3500.00"},
}

func TestSeptemberOfTheSampleListsInTheOrderOfTheApp(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "september")
	res := h.call(t, http.MethodPost, "/accounts", token, map[string]any{"name": "Débito principal", "type": "debit", "currency": "PEN"})
	require.Equal(t, http.StatusCreated, res.status)
	ids := h.ids(t, token)
	// Del más antiguo al más reciente: dentro del mismo día, el último creado va primero.
	for i := len(september) - 1; i >= 0; i-- {
		m := september[i]
		res := h.call(t, http.MethodPost, "/transactions", token, map[string]any{
			"account_id": ids[m.account], "category_id": ids[m.category], "amount": m.amount,
			"description": m.text, "occurred_on": m.date,
		})
		require.Equal(t, http.StatusCreated, res.status, res.body)
	}

	var want []string
	for _, m := range september {
		want = append(want, m.text)
	}
	all := h.entries(t, token, "from=2026-09-01&to=2026-09-30&page_size=100")
	assert.Equal(t, want, descriptions(all))
	assert.Equal(t, map[string]any{"page": float64(1), "page_size": float64(100), "total": float64(16)}, all.body["meta"])

	// En páginas de 10: la segunda trae las 6 restantes.
	second := h.entries(t, token, "from=2026-09-01&to=2026-09-30&page=2&page_size=10")
	assert.Equal(t, want[10:], descriptions(second))

	super := h.entries(t, token, "from=2026-09-01&to=2026-09-30&q=SUPER")
	assert.Equal(t, []string{"Supermercado", "Supermercado"}, descriptions(super))
	cash := h.entries(t, token, "from=2026-09-01&to=2026-09-30&account_id="+ids["Efectivo"])
	assert.Equal(t, []string{"Cine", "Farmacia", "Útiles", "Restaurante", "Venta de libros", "Pasajes de bus"}, descriptions(cash))
	assert.Empty(t, h.entries(t, token, "from=2026-09-01&to=2026-09-30&q=100%25").body["data"], "% se busca tal cual")

	// Débito: 4,100.00 de ingresos − 1,840.40 de egresos. Efectivo: 100.00 − 308.20.
	assert.Equal(t, "2259.60", h.balanceOf(t, token, "Débito principal"))
	assert.Equal(t, "-208.20", h.balanceOf(t, token, "Efectivo"))

	res = h.call(t, http.MethodGet, "/entries?from=2026-10-01&to=2026-09-01", token, nil)
	assert.Equal(t, []string{"RANGE_INVALID"}, res.fieldCodes())
	res = h.call(t, http.MethodGet, "/entries?from=2026-09-01", token, nil)
	assert.Equal(t, []string{"INVALID_FORMAT"}, res.fieldCodes())
}

func TestRetryingAPostWithTheSameIDDoesNotDuplicate(t *testing.T) {
	h := newHarness(t)
	token := h.signUp(t, "txidem")
	ids := h.ids(t, token)
	payload := map[string]any{
		"id": uuid.NewString(), "account_id": ids["Efectivo"], "category_id": ids["Sueldo"],
		"amount": "3500.00", "description": "Sueldo",
	}
	res := h.call(t, http.MethodPost, "/transactions", token, payload)
	require.Equal(t, http.StatusCreated, res.status)
	res = h.call(t, http.MethodPost, "/transactions", token, payload)
	assert.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "3500.00", h.balanceOf(t, token, "Efectivo"))

	payload["amount"] = "3600.00"
	res = h.call(t, http.MethodPost, "/transactions", token, payload)
	assert.Equal(t, "ID_CONFLICT", res.errorCode())
}
