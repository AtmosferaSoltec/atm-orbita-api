package ledger_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integración del plan 12: eliminar la cuenta no deja ninguna fila del usuario.
// Está aquí porque este harness cablea todos los módulos, como main.

// userTables descubre en el catálogo todas las tablas del esquema con columna
// user_id: una tabla nueva entra sola en la prueba.
func (h *harness) userTables(t *testing.T) []string {
	t.Helper()
	rows, err := h.pool.Query(context.Background(), `
		SELECT c.table_name
		FROM information_schema.columns c
		JOIN information_schema.tables t USING (table_schema, table_name)
		WHERE c.table_schema = current_schema() AND c.column_name = 'user_id' AND t.table_type = 'BASE TABLE'
		ORDER BY c.table_name`)
	require.NoError(t, err)
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	return tables
}

// rowsOf cuenta, tabla por tabla, las filas del usuario.
func (h *harness) rowsOf(t *testing.T, tables []string, userID string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range tables {
		var n int
		// El nombre sale del catálogo, no de la petición.
		require.NoError(t, h.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM `+table+` WHERE user_id = $1`, userID).Scan(&n))
		counts[table] = n
	}
	return counts
}

func (h *harness) userID(t *testing.T, token string) string {
	t.Helper()
	res := h.call(t, http.MethodGet, "/me", token, nil)
	require.Equal(t, http.StatusOK, res.status)
	return res.data()["id"].(string)
}

// seedEverything deja al usuario con al menos una fila en cada tabla suya.
func (h *harness) seedEverything(t *testing.T, token, userID string) {
	t.Helper()
	ids := h.seedCards(t, token)
	res := h.call(t, http.MethodPost, "/credit-purchases/"+ids["Pasajes"]+"/pay", token, map[string]any{
		"account_id": ids["Débito principal"], "paid_amount": "240.00", "paid_on": "2026-10-02",
	})
	require.Equal(t, http.StatusOK, res.status, res.body)
	res = h.call(t, http.MethodPost, "/transfers", token, map[string]any{
		"from_account_id": ids["Cuenta Dólares"], "to_account_id": ids["Débito principal"],
		"from_amount": "20.00", "to_amount": "64.00", "exchange_rate": "3.20", "occurred_on": "2026-10-01",
	})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	res = h.call(t, http.MethodPost, "/transactions", token, map[string]any{
		"account_id": ids["Efectivo"], "category_id": ids["Alimentación"], "amount": "18.50", "occurred_on": "2026-10-02",
	})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	// Un código de recuperación: este harness no envía correos.
	_, err := h.pool.Exec(context.Background(), `
		INSERT INTO password_reset_codes (id, user_id, code_hash, expires_at)
		VALUES (gen_random_uuid(), $1, '\x00', now() + interval '15 minutes')`, userID)
	require.NoError(t, err)
}

func TestDeletingTheAccountLeavesNoRowOfTheUserInAnyTable(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	tables := h.userTables(t)
	require.NotEmpty(t, tables)

	token := h.signUpWith(t, "erase", map[string]string{"timezone": "America/Lima"})
	me := h.userID(t, token)
	h.seedEverything(t, token, me)
	otherToken := h.signUpWith(t, "keep", map[string]string{"timezone": "America/Lima"})
	other := h.userID(t, otherToken)
	h.seedEverything(t, otherToken, other)

	before := h.rowsOf(t, tables, me)
	for table, n := range before {
		// Una tabla nueva sin datos aquí no probaría nada: hay que sembrarla.
		require.Positive(t, n, "seedEverything no deja filas en %s", table)
	}
	otherBefore := h.rowsOf(t, tables, other)
	var sessions []string
	rows, err := h.pool.Query(context.Background(), `SELECT id::text FROM sessions WHERE user_id = $1`, me)
	require.NoError(t, err)
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		sessions = append(sessions, id)
	}
	rows.Close()

	res := h.call(t, http.MethodDelete, "/me", token, map[string]string{"password": "vidanueva123"})
	require.Equal(t, http.StatusNoContent, res.status, res.body)

	for table, n := range h.rowsOf(t, tables, me) {
		assert.Zero(t, n, "quedan filas del usuario en %s", table)
	}
	var tokens int
	require.NoError(t, h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM refresh_tokens WHERE session_id::text = ANY($1)`, sessions).Scan(&tokens))
	assert.Zero(t, tokens, "quedan tokens de renovación")

	assert.Equal(t, otherBefore, h.rowsOf(t, tables, other), "los datos de otro usuario siguen intactos")
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/home", otherToken, nil).status)
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/home", token, nil).status)
}
