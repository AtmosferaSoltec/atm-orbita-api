package ledger_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
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
	id, ok := h.people.Load(token)
	require.True(t, ok, "token sin persona")
	return id.(string)
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
	// Recibos por honorarios (plan 18): un recibo, su tope y un archivo.
	res = h.call(t, http.MethodPost, "/receipts", token, map[string]any{
		"series": "E001", "number": "109", "issue_date": "2026-09-07", "gross": "2000.00", "source": "manual",
		"client": map[string]string{"name": "Cliente S.A.C.", "doc_type": "RUC", "doc_number": "20492684679"},
	})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	receiptID := res.data()["id"].(string)
	res = h.call(t, http.MethodPut, "/receipt-limits/2026", token, map[string]string{"amount": "48000.00"})
	require.Equal(t, http.StatusOK, res.status, res.body)
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, err := writer.CreateFormFile("file", "recibo.pdf")
	require.NoError(t, err)
	_, _ = part.Write([]byte("%PDF-1.4"))
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/receipts/"+receiptID+"/documents/pdf", &form)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	// Services, members and their payments are removed by the user cascade.
	_, err = h.pool.Exec(context.Background(), `
 WITH new_service AS (
  INSERT INTO services(id,user_id,name,color,currency,amount,billing_day,started_on,status,shared)
  VALUES(gen_random_uuid(),$1,'Deletion probe','#1D4ED8','PEN',30,10,'2026-01-01','active',true) RETURNING id,user_id
 ), new_member AS (
  INSERT INTO service_members(id,service_id,user_id,name,amount,since_month)
  SELECT gen_random_uuid(),id,user_id,'Probe member',10,'2026-01-01' FROM new_service RETURNING id,service_id,user_id
 ) INSERT INTO service_member_payments(id,service_id,user_id,member_id,month,paid_on,amount)
 SELECT gen_random_uuid(),service_id,user_id,id,'2026-01-01','2026-01-10',10 FROM new_member`, userID)
	require.NoError(t, err)
	// Loans: a debtor, a payment movement and its monthly plan go with the user cascade.
	_, err = h.pool.Exec(context.Background(), `
 WITH debtor AS (
  INSERT INTO loan_debtors(id,user_id,name,currency)
  VALUES(gen_random_uuid(),$1,'Deletion probe debtor','PEN') RETURNING id,user_id
 ), movement AS (
  INSERT INTO loan_movements(id,debtor_id,user_id,kind,amount,occurred_on,applied_month)
  SELECT gen_random_uuid(),id,user_id,'payment',50,'2026-01-10','2026-01-01' FROM debtor RETURNING id
 ) INSERT INTO loan_month_plans(debtor_id,user_id,month,amount)
 SELECT id,user_id,'2026-01-01',50 FROM debtor`, userID)
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
	// El borrado de la cuenta lo hace centralauth (ganchos de borrado y cascada).
	meID, err := uuid.Parse(me)
	require.NoError(t, err)
	require.NoError(t, h.central.Users.Delete(context.Background(), meID))

	for table, n := range h.rowsOf(t, tables, me) {
		assert.Zero(t, n, "quedan filas del usuario en %s", table)
	}
	assert.Equal(t, otherBefore, h.rowsOf(t, tables, other), "los datos de otro usuario siguen intactos")
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/home", otherToken, nil).status)
	var users int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE id = $1`, me).Scan(&users))
	assert.Zero(t, users, "queda la fila de users")
}
