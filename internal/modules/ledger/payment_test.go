package ledger_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integración del plan 10: pagar una compra con tarjeta (credit + ledger en una
// sola transacción) y deshacerlo al eliminar el egreso.

func (h *harness) pendingIDs(t *testing.T, token string) []string {
	t.Helper()
	res := h.call(t, http.MethodGet, "/credit-purchases", token, nil)
	require.Equal(t, http.StatusOK, res.status)
	var out []string
	for _, item := range res.body["data"].([]any) {
		out = append(out, item.(map[string]any)["id"].(string))
	}
	return out
}

func TestPayingAPurchaseCreatesItsExpenseAndDeletingItUndoesThePayment(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "pay", map[string]string{"timezone": "America/Lima"})
	ids := h.seedCards(t, token)
	pasajes := ids["Pasajes"]

	res := h.call(t, http.MethodPost, "/credit-purchases/"+pasajes+"/pay", token, map[string]any{
		"account_id": ids["Débito principal"], "paid_amount": "240.00", "paid_on": "2026-10-02",
	})
	require.Equal(t, http.StatusOK, res.status, res.body)
	purchase := res.data()["purchase"].(map[string]any)
	expense := res.data()["transaction"].(map[string]any)
	assert.Equal(t, "paid", purchase["status"])
	assert.Equal(t, "Pasajes", expense["description"])
	assert.Equal(t, "Transporte", expense["category"].(map[string]any)["name"])
	assert.Equal(t, pasajes, expense["credit_purchase_id"])
	assert.Equal(t, "1005.80", h.balanceOf(t, token, "Débito principal"))
	assert.NotContains(t, h.pendingIDs(t, token), pasajes)
	assert.Equal(t, []string{"PEN 96.00"}, debts(h.card(t, token, "Visa Clásica")), "Visa solo debe la cena")

	// Cuenta como egreso en la lista y en el reporte, con la fecha de pago.
	list := h.entries(t, token, "from=2026-10-01&to=2026-10-31").body["data"].([]any)
	require.Len(t, list, 1)
	assert.Equal(t, pasajes, list[0].(map[string]any)["credit_purchase_id"])
	assert.Equal(t, "240.00", block(h.report(t, token, "2026-10-01", "2026-10-31"), 0)["expense_total"])

	// Una compra pagada no se paga, edita ni elimina de nuevo.
	res = h.call(t, http.MethodPost, "/credit-purchases/"+pasajes+"/pay", token, map[string]any{
		"account_id": ids["Débito principal"], "paid_amount": "240.00",
	})
	assert.Equal(t, http.StatusConflict, res.status)
	assert.Equal(t, "PURCHASE_ALREADY_PAID", res.errorCode())
	assert.Equal(t, http.StatusConflict, h.call(t, http.MethodPatch, "/credit-purchases/"+pasajes, token, map[string]any{"amount": "1.00"}).status)
	assert.Equal(t, http.StatusConflict, h.call(t, http.MethodDelete, "/credit-purchases/"+pasajes, token, nil).status)

	// Del egreso solo cambian la cuenta, el monto y la fecha.
	expenseID := expense["id"].(string)
	res = h.call(t, http.MethodPatch, "/transactions/"+expenseID, token, map[string]any{
		"account_id": ids["Efectivo"], "amount": "238.50", "occurred_on": "2026-10-01",
	})
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Equal(t, "1245.80", h.balanceOf(t, token, "Débito principal"))
	assert.Equal(t, "-238.50", h.balanceOf(t, token, "Efectivo"))
	for _, change := range []map[string]any{{"category_id": ids["Ocio"]}, {"description": "Otra cosa"}} {
		res = h.call(t, http.MethodPatch, "/transactions/"+expenseID, token, change)
		assert.Equal(t, http.StatusUnprocessableEntity, res.status, change)
		assert.Equal(t, "PAYMENT_EXPENSE_LOCKED", res.errorCode())
	}

	// Eliminar el egreso devuelve la compra a pendiente y a la deuda de su tarjeta.
	assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/transactions/"+expenseID, token, nil).status)
	assert.Contains(t, h.pendingIDs(t, token), pasajes)
	assert.Equal(t, []string{"PEN 336.00"}, debts(h.card(t, token, "Visa Clásica")))
	assert.Equal(t, "0.00", h.balanceOf(t, token, "Efectivo"))

	// Y se puede pagar otra vez.
	res = h.call(t, http.MethodPost, "/credit-purchases/"+pasajes+"/pay", token, map[string]any{
		"account_id": ids["Débito principal"], "paid_amount": "240.00",
	})
	assert.Equal(t, http.StatusOK, res.status)
}

func TestTwoSimultaneousPaymentsLeaveASingleExpense(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "payrace", map[string]string{"timezone": "America/Lima"})
	ids := h.seedCards(t, token)

	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Go(func() {
			res := h.call(t, http.MethodPost, "/credit-purchases/"+ids["Pasajes"]+"/pay", token, map[string]any{
				"account_id": ids["Débito principal"], "paid_amount": "240.00",
			})
			statuses[i] = res.status
		})
	}
	wg.Wait()
	assert.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, statuses)
	assert.Equal(t, float64(1), h.entries(t, token, "from=2026-10-01&to=2026-10-31").body["meta"].(map[string]any)["total"])
	assert.Equal(t, "1005.80", h.balanceOf(t, token, "Débito principal"))
}

func TestAFailedPaymentLeavesThePurchasePending(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "payfail", map[string]string{"timezone": "America/Lima"})
	ids := h.seedCards(t, token)

	res := h.call(t, http.MethodPost, "/accounts", token, map[string]any{"name": "Vieja", "type": "cash", "currency": "PEN"})
	require.Equal(t, http.StatusCreated, res.status)
	old := res.data()["id"].(string)
	h.call(t, http.MethodPost, "/accounts/"+old+"/archive", token, nil)

	// La compra se bloquea y luego falla el egreso: nada queda a medias.
	res = h.call(t, http.MethodPost, "/credit-purchases/"+ids["Pasajes"]+"/pay", token, map[string]any{"account_id": old, "paid_amount": "240.00"})
	assert.Equal(t, http.StatusUnprocessableEntity, res.status)
	assert.Equal(t, "ACCOUNT_ARCHIVED", res.errorCode())
	assert.Contains(t, h.pendingIDs(t, token), ids["Pasajes"])
	assert.Equal(t, float64(0), h.entries(t, token, "from=2026-10-01&to=2026-10-31").body["meta"].(map[string]any)["total"])

	res = h.call(t, http.MethodPost, "/credit-purchases/"+ids["Pasajes"]+"/pay", token, map[string]any{"paid_amount": "0", "paid_on": "2026-10-03"})
	assert.Equal(t, []string{"ACCOUNT_REQUIRED", "AMOUNT_REQUIRED", "FUTURE_DATE"}, res.fieldCodes())

	other := h.signUp(t, "payother")
	res = h.call(t, http.MethodPost, "/credit-purchases/"+ids["Pasajes"]+"/pay", other, map[string]any{
		"account_id": h.ids(t, other)["Efectivo"], "paid_amount": "240.00",
	})
	assert.Equal(t, http.StatusNotFound, res.status)
}

func TestDeletingThePaymentOfAnArchivedCardBringsTheCardBack(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "payarch", map[string]string{"timezone": "America/Lima"})
	ids := h.seedCards(t, token)

	var expenses []string
	for _, p := range []string{"Pasajes", "Cena en restaurante"} {
		res := h.call(t, http.MethodPost, "/credit-purchases/"+ids[p]+"/pay", token, map[string]any{
			"account_id": ids["Débito principal"], "paid_amount": "1.00",
		})
		require.Equal(t, http.StatusOK, res.status, res.body)
		expenses = append(expenses, res.data()["transaction"].(map[string]any)["id"].(string))
	}
	require.Equal(t, http.StatusNoContent, h.call(t, http.MethodPost, "/credit-cards/"+ids["Visa Clásica"]+"/archive", token, nil).status)
	require.Len(t, h.cards(t, token), 1)

	assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/transactions/"+expenses[0], token, nil).status)
	cards := h.cards(t, token)
	require.Len(t, cards, 2, "la deuda no queda oculta")
	assert.Equal(t, "Visa Clásica", cards[0]["name"])
}

func (h *harness) card(t *testing.T, token, name string) map[string]any {
	t.Helper()
	for _, c := range h.cards(t, token) {
		if c["name"] == name {
			return c
		}
	}
	t.Fatalf("no card %q", name)
	return nil
}
