// Package http traduce HTTP ⇄ el caso de uso de Inicio.
package http

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/home/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Service es el caso de uso que usa el handler.
type Service interface {
	Home(ctx context.Context, userID uuid.UUID) (app.Home, error)
}

// Handler atiende Inicio.
type Handler struct {
	service Service
}

// NewHandler crea el handler.
func NewHandler(service Service) *Handler { return &Handler{service: service} }

// Routes monta la ruta, privada.
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/home", h.home)
}

type settingsResponse struct {
	MainCurrency      string      `json:"main_currency"`
	SecondaryCurrency string      `json:"secondary_currency"`
	DisplayCurrency   string      `json:"display_currency"`
	FxConfigured      bool        `json:"fx_configured"`
	Rate              *money.Rate `json:"rate"`
	FxMode            string      `json:"fx_mode"`
}

type monthResponse struct {
	From         dates.LocalDate `json:"from"`
	To           dates.LocalDate `json:"to"`
	Currency     string          `json:"currency"`
	IncomeTotal  money.Amount    `json:"income_total"`
	ExpenseTotal money.Amount    `json:"expense_total"`
}

type creditResponse struct {
	PendingCount int             `json:"pending_count"`
	NextDueDate  dates.LocalDate `json:"next_due_date"`
	HasUrgent    bool            `json:"has_urgent"`
}

type homeResponse struct {
	Today         dates.LocalDate  `json:"today"`
	Settings      settingsResponse `json:"settings"`
	Savings       any              `json:"savings"`
	Month         monthResponse    `json:"month"`
	RecentEntries []any            `json:"recent_entries"`
	Credit        *creditResponse  `json:"credit"`
}

func (h *Handler) home(c *gin.Context) {
	principal, _ := auth.FromContext(c.Request.Context())
	home, err := h.service.Home(c.Request.Context(), principal.UserID)
	if err != nil {
		apierror.Respond(c, err)
		return
	}
	out := homeResponse{
		Today: home.Today,
		Settings: settingsResponse{
			MainCurrency:      home.Settings.Main,
			SecondaryCurrency: home.Settings.Secondary,
			DisplayCurrency:   home.Settings.Display,
			FxConfigured:      home.Settings.Rate != nil,
			FxMode:            home.Settings.FxMode,
		},
		Savings: home.Savings,
		Month: monthResponse{
			From: home.Month.From, To: home.Month.To, Currency: home.Month.Currency,
			IncomeTotal: money.NewAmount(home.Month.Income), ExpenseTotal: money.NewAmount(home.Month.Expense),
		},
		RecentEntries: home.Recent,
	}
	if home.Settings.Rate != nil {
		rate := money.NewRate(*home.Settings.Rate)
		out.Settings.Rate = &rate
	}
	if home.Credit != nil {
		out.Credit = &creditResponse{
			PendingCount: home.Credit.PendingCount, NextDueDate: home.Credit.NextDueDate, HasUrgent: home.Credit.HasUrgent,
		}
	}
	apierror.OK(c, out)
}
