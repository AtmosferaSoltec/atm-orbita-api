// Package http traduce HTTP ⇄ el caso de uso de los reportes.
package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Service es el caso de uso que usa el handler.
type Service interface {
	Summary(ctx context.Context, userID uuid.UUID, from, to string) (app.Summary, error)
	PreparePDF(ctx context.Context, userID uuid.UUID, from, to string) (app.Summary, error)
	RenderPDF(ctx context.Context, summary app.Summary) ([]byte, error)
}

// Quota limita cuántos PDF genera una persona (platform/middleware.QuotaLimiter).
type Quota interface {
	Allow(key string) (retryAfter time.Duration, ok bool)
}

// Errores propios del PDF. Los mensajes no repiten nada del servicio de documentos.
var (
	errDocumentsDisabled = apierror.New(http.StatusServiceUnavailable, "DOCUMENTS_UNAVAILABLE",
		"El PDF no está disponible por ahora. Inténtalo más tarde.")
	errDocumentsFailed = apierror.New(http.StatusBadGateway, "DOCUMENTS_UNAVAILABLE",
		"No pudimos generar el PDF. Inténtalo en un momento.")
	errReportEmpty = apierror.New(http.StatusUnprocessableEntity, "REPORT_EMPTY",
		"No hay movimientos en este periodo para generar el PDF.")
	errPDFLimit = apierror.New(http.StatusTooManyRequests, apierror.CodeTooManyRequests,
		"Ya generaste el máximo de PDF por hora. Inténtalo más tarde.")
)

// Handler atiende la pantalla Reportes.
type Handler struct {
	service Service
	quota   Quota
}

// NewHandler crea el handler. quota puede ser nil (sin límite, solo pruebas).
func NewHandler(service Service, quota Quota) *Handler {
	return &Handler{service: service, quota: quota}
}

// Routes monta las rutas, privadas.
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/reports/summary", h.summary)
	protected.GET("/reports/summary.pdf", h.summaryPDF)
}

type categoryResponse struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Kind  string    `json:"kind"`
	Color string    `json:"color"`
}

type lineResponse struct {
	Category categoryResponse `json:"category"`
	Total    money.Amount     `json:"total"`
	// Percent viaja como texto con 1 decimal ("37.2"), como el dinero.
	Percent string `json:"percent"`
}

type currencyResponse struct {
	Currency     string         `json:"currency"`
	IncomeTotal  money.Amount   `json:"income_total"`
	ExpenseTotal money.Amount   `json:"expense_total"`
	Balance      money.Amount   `json:"balance"`
	Expenses     []lineResponse `json:"expenses"`
	Incomes      []lineResponse `json:"incomes"`
}

type summaryResponse struct {
	From       dates.LocalDate    `json:"from"`
	To         dates.LocalDate    `json:"to"`
	Currencies []currencyResponse `json:"currencies"`
}

func (h *Handler) summary(c *gin.Context) {
	principal, _ := auth.FromContext(c.Request.Context())
	summary, err := h.service.Summary(c.Request.Context(), principal.UserID, c.Query("from"), c.Query("to"))
	if err != nil {
		respondError(c, err)
		return
	}
	out := summaryResponse{From: summary.From, To: summary.To, Currencies: make([]currencyResponse, 0, len(summary.Currencies))}
	for _, r := range summary.Currencies {
		out.Currencies = append(out.Currencies, currencyResponse{
			Currency:     r.Currency,
			IncomeTotal:  money.NewAmount(r.IncomeTotal),
			ExpenseTotal: money.NewAmount(r.ExpenseTotal),
			Balance:      money.NewAmount(r.Balance),
			Expenses:     lines(r.Expenses),
			Incomes:      lines(r.Incomes),
		})
	}
	apierror.OK(c, out)
}

// summaryPDF devuelve el reporte como PDF. Las cifras salen del mismo caso de uso
// que /reports/summary; el dibujo lo hace atm-documents-api y nada se guarda.
func (h *Handler) summaryPDF(c *gin.Context) {
	ctx := c.Request.Context()
	principal, _ := auth.FromContext(ctx)
	summary, err := h.service.PreparePDF(ctx, principal.UserID, c.Query("from"), c.Query("to"))
	if err != nil {
		respondError(c, err)
		return
	}
	// El cupo se gasta recién aquí: un periodo inválido o vacío no cuenta.
	if h.quota != nil {
		if retryAfter, ok := h.quota.Allow(principal.UserID.String()); !ok {
			c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			apierror.Respond(c, errPDFLimit)
			return
		}
	}
	pdf, err := h.service.RenderPDF(ctx, summary)
	if err != nil {
		if errors.Is(err, app.ErrPDFFailed) {
			// Estado y code del servicio de documentos, nunca su cuerpo.
			logger.From(ctx).Warn("reports: pdf generation failed", "error", err)
		}
		respondError(c, err)
		return
	}
	// El nombre sale solo de las fechas ya validadas, nunca de texto de la persona.
	name := "orbita-" + summary.From.String() + "_" + summary.To.String() + ".pdf"
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func respondError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		fields := make([]apierror.FieldError, 0, len(validation.Fields))
		for _, f := range validation.Fields {
			fields = append(fields, apierror.FieldError{Field: f.Field, Code: f.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.Is(err, app.ErrPDFDisabled):
		apierror.Respond(c, errDocumentsDisabled)
	case errors.Is(err, app.ErrPDFFailed):
		apierror.Respond(c, errDocumentsFailed)
	case errors.Is(err, app.ErrNothingToReport):
		apierror.Respond(c, errReportEmpty)
	default:
		apierror.Respond(c, err)
	}
}

func lines(in []domain.Line) []lineResponse {
	out := make([]lineResponse, 0, len(in))
	for _, l := range in {
		out = append(out, lineResponse{
			Category: categoryResponse{ID: l.Category.ID, Name: l.Category.Name, Kind: l.Category.Kind, Color: l.Category.Color},
			Total:    money.NewAmount(l.Total),
			Percent:  l.Percent.StringFixed(1),
		})
	}
	return out
}
