package ledger_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integración del PDF del reporte (plan 15): las cifras salen del mismo caso de
// uso que /reports/summary y atm-documents-api es un servidor falso.

type fakeDocuments struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []map[string]any
	bearers  []string
	status   int // 0 = 200
}

func newFakeDocuments(t *testing.T) *fakeDocuments {
	t.Helper()
	f := &fakeDocuments{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "service-token", "expires_in": 900})
	})
	mux.HandleFunc("POST /v1/documents/period-report", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.requests = append(f.requests, body)
		f.bearers = append(f.bearers, r.Header.Get("Authorization"))
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"error":{"code":"render_failed","message":"secret upstream detail 3500.00"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 fake"))
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

type rawResponse struct {
	status int
	header http.Header
	body   string
}

func (h *harness) get(t *testing.T, path, token string) rawResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rawResponse{status: rec.Code, header: rec.Header(), body: rec.Body.String()}
}

func (r rawResponse) errorCode(t *testing.T) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.body), &body), r.body)
	return body.Error.Code
}

func TestReportPDFSendsTheSummaryFiguresAndStreamsThePDF(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "reportpdf", map[string]string{"timezone": "America/Lima"})
	h.seedSample(t, token)

	res := h.get(t, "/reports/summary.pdf?from=2026-09-01&to=2026-09-30", token)
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Equal(t, "%PDF-1.4 fake", res.body)
	assert.Equal(t, "application/pdf", res.header.Get("Content-Type"))
	assert.Equal(t, `attachment; filename="orbita-2026-09-01_2026-09-30.pdf"`, res.header.Get("Content-Disposition"))
	assert.Equal(t, "no-store", res.header.Get("Cache-Control"))

	require.Len(t, h.documents.requests, 1)
	assert.Equal(t, "Bearer service-token", h.documents.bearers[0])
	sent := h.documents.requests[0]
	assert.Equal(t, "2026-09-01", sent["from"])
	assert.Equal(t, "2026-09-30", sent["to"])
	pen := sent["currencies"].([]any)[0].(map[string]any)
	assert.Equal(t, "PEN", pen["currency"])
	assert.Equal(t, "4200.00", pen["income_total"])
	assert.Equal(t, "2148.60", pen["expense_total"])
	assert.Equal(t, "2051.40", pen["balance"])
	first := pen["expenses"].([]any)[0].(map[string]any)
	assert.Equal(t, map[string]any{"name": "Vivienda", "total": "800.00", "percent": "37.2"}, first)

	// Las mismas cifras que el JSON.
	summary := h.report(t, token, "2026-09-01", "2026-09-30")
	assert.Equal(t, block(summary, 0)["expense_total"], pen["expense_total"])
}

func TestReportPDFClipsToTodayAndNamesTheFileFromThatDate(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "reportpdfto", map[string]string{"timezone": "America/Lima"})
	h.seedSample(t, token)

	res := h.get(t, "/reports/summary.pdf?from=2026-10-01&to=2026-10-31", token)
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Equal(t, `attachment; filename="orbita-2026-10-01_2026-10-02.pdf"`, res.header.Get("Content-Disposition"))
	assert.Equal(t, "2026-10-02", h.documents.requests[0]["to"])
}

func TestReportPDFValidatesLikeTheSummary(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "reportpdfbad", map[string]string{"timezone": "America/Lima"})
	h.seedSample(t, token)

	for _, query := range []string{
		"from=2026-10-31&to=2026-10-01", // al revés
		"from=hoy&to=2026-10-01",        // mal formada
		"from=2026-10-01",               // falta to
		"from=2020-01-01&to=2026-10-01", // más de 366 días (y menos de 10 años)
	} {
		res := h.get(t, "/reports/summary.pdf?"+query, token)
		assert.Equal(t, http.StatusBadRequest, res.status, query)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(t), query)
		assert.Contains(t, res.body, "RANGE_INVALID", query)
	}
	assert.Equal(t, http.StatusUnauthorized, h.get(t, "/reports/summary.pdf?from=2026-09-01&to=2026-09-30", "").status)
	assert.Empty(t, h.documents.requests, "nothing reached documents-api")

	empty := h.get(t, "/reports/summary.pdf?from=2026-08-01&to=2026-08-31", token)
	assert.Equal(t, http.StatusUnprocessableEntity, empty.status)
	assert.Equal(t, "REPORT_EMPTY", empty.errorCode(t))
	assert.Empty(t, h.documents.requests)
}

func TestReportPDFAnswers502WithoutLeakingTheUpstreamBody(t *testing.T) {
	h := newHarnessAt(t, sampleToday)
	token := h.signUpWith(t, "reportpdf502", map[string]string{"timezone": "America/Lima"})
	h.seedSample(t, token)
	h.documents.status = http.StatusInternalServerError

	res := h.get(t, "/reports/summary.pdf?from=2026-09-01&to=2026-09-30", token)
	assert.Equal(t, http.StatusBadGateway, res.status)
	assert.Equal(t, "DOCUMENTS_UNAVAILABLE", res.errorCode(t))
	assert.NotContains(t, res.body, "secret upstream detail")
	assert.NotContains(t, res.body, "render_failed")
}

func TestReportPDFIsLimitedPerPerson(t *testing.T) {
	h := newHarnessAt(t, sampleToday) // el harness permite 3 por hora
	token := h.signUpWith(t, "reportpdflimit", map[string]string{"timezone": "America/Lima"})
	other := h.signUpWith(t, "reportpdfother", map[string]string{"timezone": "America/Lima"})
	h.seedSample(t, token)
	h.seedSample(t, other)

	path := "/reports/summary.pdf?from=2026-09-01&to=2026-09-30"
	for range 3 {
		require.Equal(t, http.StatusOK, h.get(t, path, token).status)
	}
	limited := h.get(t, path, token)
	assert.Equal(t, http.StatusTooManyRequests, limited.status)
	assert.Equal(t, "TOO_MANY_REQUESTS", limited.errorCode(t))
	assert.NotEmpty(t, limited.header.Get("Retry-After"))
	assert.Len(t, h.documents.requests, 3, "the rejected one never reached documents-api")

	assert.Equal(t, http.StatusOK, h.get(t, path, other).status, "the limit is per person")
}
