// Package documents es el cliente de atm-documents-api, que dibuja los PDF.
// Orbita no tiene lógica de PDF: arma las cifras, las envía con el token de
// servicio y devuelve los bytes que reciba.
package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/servicetoken"
)

// ErrNotConfigured: falta AUTH_CLIENT_SECRET o la URL; el PDF queda apagado.
var ErrNotConfigured = errors.New("documents api is not configured")

// ErrUnavailable: documents-api no pudo entregar el PDF (red, 5xx, rechazo o
// respuesta que no es un PDF). El detalle va en el error envuelto, sin cuerpos.
var ErrUnavailable = errors.New("documents api unavailable")

// maxPDFBytes acota lo que se acepta de vuelta: un reporte son unas pocas páginas.
const maxPDFBytes = 10 << 20

// Line es una fila de un ranking.
type Line struct {
	Name    string `json:"name"`
	Total   string `json:"total"`
	Percent string `json:"percent"`
}

// Currency es el bloque de una moneda. Los importes son texto con 2 decimales y
// el porcentaje con 1, como los espera documents-api.
type Currency struct {
	Currency     string `json:"currency"`
	IncomeTotal  string `json:"income_total"`
	ExpenseTotal string `json:"expense_total"`
	Balance      string `json:"balance"`
	Expenses     []Line `json:"expenses"`
	Incomes      []Line `json:"incomes"`
}

// PeriodReport es el cuerpo de POST /v1/documents/period-report.
type PeriodReport struct {
	From       string     `json:"from"`
	To         string     `json:"to"`
	Currencies []Currency `json:"currencies"`
}

// Client llama a atm-documents-api.
type Client struct {
	baseURL string
	tokens  *servicetoken.Source
	http    *http.Client
}

// NewClient crea el cliente. baseURL es DOCUMENTS_API_URL.
func NewClient(baseURL string, tokens *servicetoken.Source) *Client {
	// documents-api corta a los 15 s; este tope da margen sin pasar el de escritura del servidor.
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), tokens: tokens, http: &http.Client{Timeout: 17 * time.Second}}
}

// Configured dice si hay URL y credenciales de servicio.
func (c *Client) Configured() bool { return c != nil && c.baseURL != "" && c.tokens.Configured() }

// PeriodReportPDF pide el PDF del reporte de un periodo.
func (c *Client) PeriodReportPDF(ctx context.Context, report PeriodReport) ([]byte, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	res, err := c.tokens.Do(ctx, c.http, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/documents/period-report", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/pdf")
		return req, nil
	})
	if errors.Is(err, servicetoken.ErrNotConfigured) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("%w: request failed", ErrUnavailable)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		// Solo el estado y el code de documents-api; el cuerpo puede repetir cifras.
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
		return nil, fmt.Errorf("%w: status %d %s", ErrUnavailable, res.StatusCode, body.Error.Code)
	}
	pdf, err := io.ReadAll(io.LimitReader(res.Body, maxPDFBytes+1))
	if err != nil || len(pdf) > maxPDFBytes || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, fmt.Errorf("%w: response is not a valid pdf", ErrUnavailable)
	}
	return pdf, nil
}
