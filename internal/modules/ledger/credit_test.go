package ledger_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integración del módulo credit (plan 09): tarjetas y compras pendientes.

// seedCards crea las dos tarjetas y las cuatro compras pendientes de
// SampleData.kt. La "Suscripción de software" se compra en dólares: Mastercard
// nace en USD y luego pasa a soles, y la compra conserva su moneda.
func (h *harness) seedCards(t *testing.T, token string) map[string]string {
	t.Helper()
	ids := h.sampleAccounts(t, token)
	post := func(path string, body map[string]any) string {
		res := h.call(t, http.MethodPost, path, token, body)
		require.Equal(t, http.StatusCreated, res.status, res.body)
		return res.data()["id"].(string)
	}
	ids["Visa Clásica"] = post("/credit-cards", map[string]any{"name": "Visa Clásica", "currency": "PEN"})
	ids["Mastercard Oro"] = post("/credit-cards", map[string]any{"name": "Mastercard Oro", "currency": "USD"})
	ids["Suscripción de software"] = post("/credit-purchases", map[string]any{
		"card_id": ids["Mastercard Oro"], "category_id": ids["Otros"], "amount": "12.00",
		"description": "Suscripción de software", "due_date": "2026-10-15",
	})
	res := h.call(t, http.MethodPatch, "/credit-cards/"+ids["Mastercard Oro"], token, map[string]any{"currency": "PEN"})
	require.Equal(t, http.StatusOK, res.status, res.body)
	for _, p := range []struct{ card, category, amount, text, due string }{
		{"Visa Clásica", "Transporte", "240.00", "Pasajes", "2026-10-05"},
		{"Visa Clásica", "Alimentación", "96.00", "Cena en restaurante", "2026-10-15"},
		{"Mastercard Oro", "Ocio", "189.90", "Audífonos", "2026-10-15"},
	} {
		ids[p.text] = post("/credit-purchases", map[string]any{
			"card_id": ids[p.card], "category_id": ids[p.category], "amount": p.amount, "description": p.text, "due_date": p.due,
		})
	}
	return ids
}

func (h *harness) cards(t *testing.T, token string) []map[string]any {
	t.Helper()
	res := h.call(t, http.MethodGet, "/credit-cards", token, nil)
	require.Equal(t, http.StatusOK, res.status, res.body)
	var out []map[string]any
	for _, item := range res.body["data"].([]any) {
		out = append(out, item.(map[string]any))
	}
	return out
}

func debts(card map[string]any) []string {
	var out []string
	for _, d := range card["debt"].([]any) {
		debt := d.(map[string]any)
		out = append(out, debt["currency"].(string)+" "+debt["amount"].(string))
	}
	return out
}

func TestCardDebtOfTheSampleAndAPurchaseTouchesNoBalance(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "cards", map[string]string{"timezone": "America/Lima"})
	ids := h.seedCards(t, token)

	cards := h.cards(t, token)
	require.Len(t, cards, 2)
	assert.Equal(t, "Visa Clásica", cards[0]["name"], "la de fecha límite más próxima primero")
	assert.Equal(t, []string{"PEN 336.00"}, debts(cards[0]))
	assert.Equal(t, float64(2), cards[0]["pending_count"])
	assert.Equal(t, "2026-10-05", cards[0]["next_due_date"])
	assert.Equal(t, []string{"PEN 189.90", "USD 12.00"}, debts(cards[1]))

	res := h.call(t, http.MethodGet, "/credit-purchases?status=pending", token, nil)
	require.Equal(t, http.StatusOK, res.status)
	list := res.body["data"].([]any)
	require.Len(t, list, 4)
	first := list[0].(map[string]any)
	assert.Equal(t, "Pasajes", first["description"])
	assert.Equal(t, "2026-10-02", first["purchase_date"], "la fecha de compra es hoy")
	assert.Equal(t, "pending", first["status"])
	assert.Equal(t, "Transporte", first["category"].(map[string]any)["name"])
	assert.Equal(t, "Visa Clásica", first["card"].(map[string]any)["name"])

	// Una compra pendiente no es un movimiento: ningún saldo ni reporte cambia.
	assert.Equal(t, "1245.80", h.balanceOf(t, token, "Débito principal"))
	assert.Empty(t, h.report(t, token, "2026-10-01", "2026-10-31")["currencies"])
	assert.Equal(t, float64(0), h.entries(t, token, "from=2026-01-01&to=2026-12-31").body["meta"].(map[string]any)["total"])

	// Editar el monto de Pasajes solo cambia la deuda de Visa.
	res = h.call(t, http.MethodPatch, "/credit-purchases/"+ids["Pasajes"], token, map[string]any{"amount": "250.00"})
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Equal(t, []string{"PEN 346.00"}, debts(h.cards(t, token)[0]))
	assert.Equal(t, "1245.80", h.balanceOf(t, token, "Débito principal"))
}

func TestMovingAPurchaseTakesTheCurrencyOfTheNewCard(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "cardmove", map[string]string{"timezone": "America/Lima"})
	ids := h.seedCards(t, token)

	// Quedarse en su tarjeta, que ya es en soles, conserva sus dólares.
	res := h.call(t, http.MethodPatch, "/credit-purchases/"+ids["Suscripción de software"], token, map[string]any{"description": "Suscripción anual"})
	require.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "USD", res.data()["currency"])

	res = h.call(t, http.MethodPatch, "/credit-purchases/"+ids["Suscripción de software"], token, map[string]any{"card_id": ids["Visa Clásica"]})
	require.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "PEN", res.data()["currency"])
	assert.Equal(t, []string{"PEN 348.00"}, debts(h.cards(t, token)[0]))

	// La fecha límite puede quedar vencida al editar, pero no antes de la compra.
	res = h.call(t, http.MethodPatch, "/credit-purchases/"+ids["Pasajes"], token, map[string]any{"due_date": "2026-10-02"})
	assert.Equal(t, http.StatusOK, res.status)
	res = h.call(t, http.MethodPatch, "/credit-purchases/"+ids["Pasajes"], token, map[string]any{"due_date": "2026-10-01"})
	assert.Equal(t, []string{"DUE_DATE_PAST"}, res.fieldCodes())
}

func TestACardWithPendingPurchasesIsNotArchived(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "cardarch", map[string]string{"timezone": "America/Lima"})
	ids := h.seedCards(t, token)

	res := h.call(t, http.MethodPost, "/credit-cards/"+ids["Visa Clásica"]+"/archive", token, nil)
	assert.Equal(t, http.StatusConflict, res.status)
	assert.Equal(t, "CARD_HAS_PENDING_PURCHASES", res.errorCode())

	for _, p := range []string{"Pasajes", "Cena en restaurante"} {
		assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/credit-purchases/"+ids[p], token, nil).status)
	}
	assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/credit-purchases/"+ids["Pasajes"], token, nil).status, "eliminar dos veces")
	assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodPost, "/credit-cards/"+ids["Visa Clásica"]+"/archive", token, nil).status)
	assert.Len(t, h.cards(t, token), 1)

	res = h.call(t, http.MethodPost, "/credit-purchases", token, map[string]any{
		"card_id": ids["Visa Clásica"], "category_id": ids["Ocio"], "amount": "1.00", "due_date": "2026-10-20",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, res.status)
	assert.Equal(t, "CARD_ARCHIVED", res.errorCode())
}

func TestPurchaseValidationAndOwnership(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	alice := h.signUpWith(t, "cardalice", map[string]string{"timezone": "America/Lima"})
	ids := h.seedCards(t, alice)

	res := h.call(t, http.MethodPost, "/credit-purchases", alice, map[string]any{"amount": "0", "due_date": "2026-10-01"})
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, []string{"AMOUNT_REQUIRED", "CARD_REQUIRED", "CATEGORY_REQUIRED", "DUE_DATE_PAST"}, res.fieldCodes())

	res = h.call(t, http.MethodPost, "/credit-purchases", alice, map[string]any{
		"card_id": ids["Visa Clásica"], "category_id": ids["Sueldo"], "amount": "10.00", "due_date": "2026-10-20",
	})
	assert.Equal(t, []string{"CATEGORY_KIND_MISMATCH"}, res.fieldCodes())

	res = h.call(t, http.MethodPost, "/credit-cards", alice, map[string]any{"name": " ", "currency": "XXX"})
	assert.Equal(t, []string{"NAME_REQUIRED", "CURRENCY_NOT_SUPPORTED"}, res.fieldCodes())

	newcomer := h.signUp(t, "cardnew")
	res = h.call(t, http.MethodPost, "/credit-cards", newcomer, map[string]any{"name": "Dólares", "currency": "USD"})
	assert.Equal(t, "FX_RATE_NOT_CONFIGURED", res.errorCode())

	bob := h.signUp(t, "cardbob")
	assert.Empty(t, h.cards(t, bob))
	res = h.call(t, http.MethodGet, "/credit-purchases", bob, nil)
	assert.Empty(t, res.body["data"])
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodPatch, "/credit-purchases/"+ids["Pasajes"], bob, map[string]any{"amount": "1.00"}).status)
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodDelete, "/credit-purchases/"+ids["Pasajes"], bob, nil).status)
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodPost, "/credit-cards/"+ids["Visa Clásica"]+"/archive", bob, nil).status)
}
