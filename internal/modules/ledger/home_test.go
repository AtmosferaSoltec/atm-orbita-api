package ledger_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integración del módulo home (plan 08): Inicio en una sola petición.

func (h *harness) home(t *testing.T, token string) map[string]any {
	t.Helper()
	res := h.call(t, http.MethodGet, "/home", token, nil)
	require.Equal(t, http.StatusOK, res.status, res.body)
	return res.data()
}

func TestHomeOfTheSample(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "home", map[string]string{"timezone": "America/Lima"})
	ids := h.seedSample(t, token)
	post := func(path string, body map[string]any) string {
		res := h.call(t, http.MethodPost, path, token, body)
		require.Equal(t, http.StatusCreated, res.status, res.body)
		return res.data()["id"].(string)
	}
	visa := post("/credit-cards", map[string]any{"name": "Visa Clásica", "currency": "PEN"})
	mastercard := post("/credit-cards", map[string]any{"name": "Mastercard Oro", "currency": "PEN"})
	for _, p := range []struct{ card, category, amount, due string }{
		{visa, "Transporte", "240.00", "2026-10-05"},
		{visa, "Alimentación", "96.00", "2026-10-15"},
		{mastercard, "Ocio", "189.90", "2026-10-15"},
		{mastercard, "Otros", "12.00", "2026-10-15"},
	} {
		post("/credit-purchases", map[string]any{"card_id": p.card, "category_id": ids[p.category], "amount": p.amount, "due_date": p.due})
	}

	home := h.home(t, token)
	assert.Equal(t, "2026-10-02", home["today"])
	settings := home["settings"].(map[string]any)
	assert.Equal(t, "PEN", settings["main_currency"])
	assert.Equal(t, "USD", settings["secondary_currency"])
	assert.Equal(t, true, settings["fx_configured"])
	assert.Equal(t, "3.200000", settings["rate"])
	assert.Equal(t, "manual", settings["fx_mode"])

	// Las cifras son exactamente las de cada endpoint.
	_, savings := h.accounts(t, token)
	assert.Equal(t, savings, home["savings"])

	month := home["month"].(map[string]any)
	assert.Equal(t, map[string]any{
		"from": "2026-10-01", "to": "2026-10-02", "currency": "PEN",
		"income_total": "3500.00", "expense_total": "30.50",
	}, month)

	var recent []string
	for _, item := range home["recent_entries"].([]any) {
		row := item.(map[string]any)
		if row["entry_type"] == "transfer" {
			recent = append(recent, row["note"].(string))
		} else {
			recent = append(recent, row["description"].(string))
		}
	}
	assert.Equal(t, []string{"Almuerzo", "Taxi al trabajo", "Sueldo", "Cambio de dólares"}, recent)
	assert.Equal(t, h.entries(t, token, "from=2016-10-03&to=2026-10-02&page_size=4").body["data"], home["recent_entries"])

	assert.Equal(t, map[string]any{"pending_count": float64(4), "next_due_date": "2026-10-05", "has_urgent": true}, home["credit"])
}

func TestHomeOfANewUser(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "homenew", map[string]string{"timezone": "America/Lima", "main_currency": "MXN"})
	home := h.home(t, token)

	settings := home["settings"].(map[string]any)
	assert.Equal(t, false, settings["fx_configured"])
	assert.Nil(t, settings["rate"])
	assert.Equal(t, []any{map[string]any{"currency": "MXN", "amount": "0.00"}}, home["savings"].(map[string]any)["totals"])
	assert.Equal(t, "0.00", home["month"].(map[string]any)["income_total"])
	assert.Equal(t, "MXN", home["month"].(map[string]any)["currency"])
	assert.Equal(t, []any{}, home["recent_entries"])
	assert.Nil(t, home["credit"])

	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/home", "", nil).status)
}
