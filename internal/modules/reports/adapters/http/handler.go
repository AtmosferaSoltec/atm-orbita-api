// Package http traduce HTTP ⇄ el caso de uso de los reportes.
package http

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Service es el caso de uso que usa el handler.
type Service interface {
	Summary(ctx context.Context, userID uuid.UUID, from, to string) (app.Summary, error)
}

// Handler atiende la pantalla Reportes.
type Handler struct {
	service Service
}

// NewHandler crea el handler.
func NewHandler(service Service) *Handler { return &Handler{service: service} }

// Routes monta la ruta, privada.
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/reports/summary", h.summary)
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
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		fields := make([]apierror.FieldError, 0, len(validation.Fields))
		for _, f := range validation.Fields {
			fields = append(fields, apierror.FieldError{Field: f.Field, Code: f.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
		return
	}
	if err != nil {
		apierror.Respond(c, err)
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
