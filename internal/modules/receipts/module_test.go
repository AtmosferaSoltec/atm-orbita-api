package receipts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/storage"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// Integración: identity + profile + receipts por HTTP contra el Postgres local,
// cableados como en cmd/api/main.go, con el almacenamiento en memoria.

type harness struct {
	router *gin.Engine
	pool   *pgxpool.Pool
	files  *storage.Memory
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := testsupport.MigratedPool(t)
	gin.SetMode(gin.TestMode)
	router, api, err := httpserver.NewRouter(httpserver.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), RateLimitPerMinute: 10_000,
		LargeBodyRoutes: map[string]int64{receipts.UploadPath: receipts.MaxUploadBytes},
	})
	require.NoError(t, err)
	runner := tx.NewRunner(pool)
	identityModule, err := identity.New(identity.Deps{
		Tx: runner, Clock: clock.System{}, JWTSecret: strings.Repeat("k", 64),
		AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: 30 * 24 * time.Hour,
		PasswordParams: &identity.FastPasswordParams,
	})
	require.NoError(t, err)
	profileModule := profile.New(runner, clock.System{})
	files := storage.NewMemory()
	receiptsModule := receipts.New(runner, clock.System{}, profileModule.Reader(), files)
	identityModule.OnUserRegistered(func(ctx context.Context, u identity.NewUser) error {
		return profileModule.OnUserRegistered(ctx, u.ID, u.MainCurrency, u.Timezone)
	})
	identityModule.OnUserDeleted(receiptsModule.OnUserDeleted)
	protected := api.Group("", auth.Require(identityModule.Authenticator()))
	identityModule.Routes(api, api, protected)
	receiptsModule.Routes(protected)
	return &harness{router: router, pool: pool, files: files}
}

type response struct {
	status int
	body   map[string]any
}

func (r response) data(t *testing.T) map[string]any {
	t.Helper()
	data, ok := r.body["data"].(map[string]any)
	require.True(t, ok, "no data object in %v", r.body)
	return data
}

func (r response) list(t *testing.T) []any {
	t.Helper()
	data, ok := r.body["data"].([]any)
	require.True(t, ok, "no data list in %v", r.body)
	return data
}

func (r response) errorCode() string {
	e, _ := r.body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func (h *harness) do(t *testing.T, req *http.Request, token string) response {
	t.Helper()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	out := response{status: rec.Code}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

func (h *harness) call(t *testing.T, method, path, token string, payload any) response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, body)
	req.Header.Set("Content-Type", "application/json")
	return h.do(t, req, token)
}

func (h *harness) upload(t *testing.T, token, receiptID, kind, fileName string, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", fileName)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, form.Close())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/receipts/"+receiptID+"/documents/"+kind, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	return h.do(t, req, token)
}

func (h *harness) register(t *testing.T) string {
	t.Helper()
	email := testsupport.UniqueEmail("receipts")
	t.Cleanup(func() { testsupport.DeleteUsersByEmail(t, h.pool, email) })
	res := h.call(t, http.MethodPost, "/auth/register", "", map[string]string{"email": email, "password": "vidanueva123"})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	return res.data(t)["tokens"].(map[string]any)["access_token"].(string)
}

func receiptBody(number string) map[string]any {
	return map[string]any{
		"series": "E001", "number": number, "issue_date": "2026-09-07",
		"client":      map[string]string{"name": "SISTEMAS Y SOLUCIONES EMPRESARIALES S.A.C.", "doc_type": "RUC", "doc_number": "20492684679"},
		"description": "SERVICIO DE PROGRAMACION", "gross": "2000.00", "withholding": "0.00", "source": "xml",
	}
}

func receiptXML(code string) []byte {
	return fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8"?>
<Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2" xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2">
	<cbc:ID>%s</cbc:ID><cbc:IssueDate>2026-09-07</cbc:IssueDate>
</Invoice>`, code)
}

func (h *harness) createReceipt(t *testing.T, token, number string) map[string]any {
	t.Helper()
	res := h.call(t, http.MethodPost, "/receipts", token, receiptBody(number))
	require.Equal(t, http.StatusCreated, res.status, res.body)
	return res.data(t)
}

func TestCreateListAndGet(t *testing.T) {
	h := newHarness(t)
	token := h.register(t)

	body := receiptBody("00109")
	body["withholding"] = "160.00"
	res := h.call(t, http.MethodPost, "/receipts", token, body)
	require.Equal(t, http.StatusCreated, res.status, res.body)
	created := res.data(t)
	assert.Equal(t, "109", created["number"], "leading zeros are dropped")
	assert.Equal(t, "2000.00", created["gross"])
	assert.Equal(t, "1840.00", created["net"])
	assert.Nil(t, created["collected_on"])
	assert.Equal(t, []any{}, created["documents"])

	list := h.call(t, http.MethodGet, "/receipts?year=2026", token, nil)
	require.Equal(t, http.StatusOK, list.status)
	assert.Len(t, list.list(t), 1)
	assert.Empty(t, h.call(t, http.MethodGet, "/receipts?year=2025", token, nil).list(t))

	got := h.call(t, http.MethodGet, "/receipts/"+created["id"].(string), token, nil)
	assert.Equal(t, "SISTEMAS Y SOLUCIONES EMPRESARIALES S.A.C.", got.data(t)["client"].(map[string]any)["name"])

	years := h.call(t, http.MethodGet, "/receipts/years", token, nil).data(t)
	assert.Contains(t, years["years"], float64(2026))
}

func TestCreateIsIdempotentByIDAndRejectsDuplicates(t *testing.T) {
	h := newHarness(t)
	token := h.register(t)

	body := receiptBody("58")
	body["id"] = uuid.NewString()
	assert.Equal(t, http.StatusCreated, h.call(t, http.MethodPost, "/receipts", token, body).status)
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodPost, "/receipts", token, body).status, "retry returns the same receipt")

	duplicate := receiptBody("058")
	duplicate["series"] = "e001"
	res := h.call(t, http.MethodPost, "/receipts", token, duplicate)
	assert.Equal(t, http.StatusConflict, res.status)
	assert.Equal(t, "RECEIPT_ALREADY_EXISTS", res.errorCode())

	// Otra persona sí puede tener ese número, pero no ese id.
	other := h.register(t)
	assert.Equal(t, http.StatusCreated, h.call(t, http.MethodPost, "/receipts", other, receiptBody("58")).status)
	assert.Equal(t, "ID_CONFLICT", h.call(t, http.MethodPost, "/receipts", other, body).errorCode())
}

func TestValidationErrorsComeWithTheirFields(t *testing.T) {
	h := newHarness(t)
	token := h.register(t)
	body := receiptBody("1")
	body["issue_date"] = "2099-01-01"
	body["client"] = map[string]string{"name": "Ana", "doc_type": "DNI", "doc_number": "123"}
	body["withholding"] = "2000.00"
	res := h.call(t, http.MethodPost, "/receipts", token, body)
	assert.Equal(t, http.StatusBadRequest, res.status)
	details := res.body["error"].(map[string]any)["details"]
	assert.Contains(t, details, map[string]any{"field": "issue_date", "code": "DATE_IN_FUTURE"})
	assert.Contains(t, details, map[string]any{"field": "client.doc_number", "code": "DNI_INVALID"})
	assert.Contains(t, details, map[string]any{"field": "withholding", "code": "WITHHOLDING_INVALID"})
}

func TestUpdateCollectAndDelete(t *testing.T) {
	h := newHarness(t)
	token := h.register(t)
	id := h.createReceipt(t, token, "10")["id"].(string)

	body := receiptBody("10")
	body["description"] = "Editado"
	assert.Equal(t, "Editado", h.call(t, http.MethodPatch, "/receipts/"+id, token, body).data(t)["description"])

	collected := h.call(t, http.MethodPut, "/receipts/"+id+"/collected", token, map[string]any{"collected_on": "2026-09-10"})
	assert.Equal(t, "2026-09-10", collected.data(t)["collected_on"])
	before := h.call(t, http.MethodPut, "/receipts/"+id+"/collected", token, map[string]any{"collected_on": "2026-09-01"})
	assert.Equal(t, http.StatusBadRequest, before.status)
	cleared := h.call(t, http.MethodPut, "/receipts/"+id+"/collected", token, map[string]any{"collected_on": nil})
	assert.Nil(t, cleared.data(t)["collected_on"])

	assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/receipts/"+id, token, nil).status)
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodGet, "/receipts/"+id, token, nil).status)
}

func TestReceiptsAreIsolatedBetweenPeople(t *testing.T) {
	h := newHarness(t)
	owner := h.register(t)
	stranger := h.register(t)
	id := h.createReceipt(t, owner, "20")["id"].(string)

	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodGet, "/receipts/"+id, stranger, nil).status)
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodPatch, "/receipts/"+id, stranger, receiptBody("20")).status)
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodDelete, "/receipts/"+id, stranger, nil).status)
	assert.Equal(t, http.StatusNotFound, h.upload(t, stranger, id, "pdf", "x.pdf", []byte("%PDF-1.4")).status)
	assert.Empty(t, h.call(t, http.MethodGet, "/receipts?year=2026", stranger, nil).list(t))
}

func TestYearLimitDefaultsAndCanBeChanged(t *testing.T) {
	h := newHarness(t)
	token := h.register(t)
	limit := h.call(t, http.MethodGet, "/receipt-limits/2026", token, nil).data(t)
	assert.Equal(t, "50000.00", limit["amount"])
	assert.Equal(t, true, limit["is_default"])

	set := h.call(t, http.MethodPut, "/receipt-limits/2026", token, map[string]string{"amount": "48000.00"})
	require.Equal(t, http.StatusOK, set.status, set.body)
	limit = h.call(t, http.MethodGet, "/receipt-limits/2026", token, nil).data(t)
	assert.Equal(t, "48000.00", limit["amount"])
	assert.Equal(t, false, limit["is_default"])
	assert.Equal(t, true, h.call(t, http.MethodGet, "/receipt-limits/2025", token, nil).data(t)["is_default"])

	assert.Equal(t, http.StatusBadRequest, h.call(t, http.MethodPut, "/receipt-limits/2026", token, map[string]string{"amount": "0.00"}).status)
	assert.Equal(t, http.StatusBadRequest, h.call(t, http.MethodGet, "/receipt-limits/abc", token, nil).status)
}

func TestDocumentsUploadReplaceAndDelete(t *testing.T) {
	h := newHarness(t)
	token := h.register(t)
	id := h.createReceipt(t, token, "109")["id"].(string)

	xml := h.upload(t, token, id, "xml", "RHE10744555189E001109.xml", receiptXML("E001-109"))
	require.Equal(t, http.StatusOK, xml.status, xml.body)
	assert.Equal(t, "RHE10744555189E001109.xml", xml.data(t)["file_name"])

	first := h.upload(t, token, id, "pdf", "recibo.pdf", []byte("%PDF-1.4 first"))
	require.Equal(t, http.StatusOK, first.status, first.body)
	second := h.upload(t, token, id, "pdf", "recibo-v2.pdf", []byte("%PDF-1.4 second"))
	require.Equal(t, http.StatusOK, second.status, second.body)
	require.Len(t, h.files.Keys(), 2, "the replaced PDF is deleted from storage")

	docs := h.call(t, http.MethodGet, "/receipts/"+id, token, nil).data(t)["documents"].([]any)
	assert.Len(t, docs, 2)

	url := h.call(t, http.MethodGet, "/receipts/"+id+"/documents/pdf/url?download=1", token, nil).data(t)
	assert.Contains(t, url["url"], "download=1")
	assert.Equal(t, float64(300), url["expires_in"])

	assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/receipts/"+id+"/documents/pdf", token, nil).status)
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodGet, "/receipts/"+id+"/documents/pdf/url", token, nil).status)
	assert.Len(t, h.files.Keys(), 1)

	// Borrar el recibo se lleva el XML de R2.
	assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/receipts/"+id, token, nil).status)
	assert.Empty(t, h.files.Keys())
}

func TestDocumentsAreChecked(t *testing.T) {
	h := newHarness(t)
	token := h.register(t)
	id := h.createReceipt(t, token, "109")["id"].(string)

	mismatch := h.upload(t, token, id, "xml", "otro.xml", receiptXML("E001-58"))
	assert.Equal(t, http.StatusUnprocessableEntity, mismatch.status)
	assert.Equal(t, "XML_RECEIPT_MISMATCH", mismatch.errorCode())

	fake := h.upload(t, token, id, "pdf", "falso.pdf", []byte("hola"))
	assert.Equal(t, http.StatusBadRequest, fake.status)
	assert.Equal(t, "FILE_INVALID", fake.errorCode())

	big := h.upload(t, token, id, "pdf", "grande.pdf", append([]byte("%PDF-"), make([]byte, 5_000_001)...))
	assert.Equal(t, http.StatusBadRequest, big.status)
	assert.Equal(t, http.StatusNotFound, h.upload(t, token, id, "docx", "x.docx", []byte("x")).status)
	assert.Empty(t, h.files.Keys())
}

func TestDeletingTheAccountDeletesItsFiles(t *testing.T) {
	h := newHarness(t)
	token := h.register(t)
	id := h.createReceipt(t, token, "109")["id"].(string)
	require.Equal(t, http.StatusOK, h.upload(t, token, id, "pdf", "recibo.pdf", []byte("%PDF-1.4")).status)
	require.Len(t, h.files.Keys(), 1)

	res := h.call(t, http.MethodDelete, "/me", token, map[string]string{"password": "vidanueva123"})
	require.Equal(t, http.StatusNoContent, res.status, res.body)
	assert.Empty(t, h.files.Keys())
}
