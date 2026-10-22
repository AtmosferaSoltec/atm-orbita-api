package http

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// AccountService son los casos de uso de cuentas que usa el handler.
type AccountService interface {
	List(ctx context.Context, userID uuid.UUID) (app.AccountList, error)
	Create(ctx context.Context, userID uuid.UUID, in app.CreateAccountInput) (domain.Account, bool, error)
	Update(ctx context.Context, userID, id uuid.UUID, in app.UpdateAccountInput) (domain.Account, error)
	Archive(ctx context.Context, userID, id uuid.UUID) error
}

// Accounts atiende la pantalla Cuentas.
type Accounts struct {
	service AccountService
}

// NewAccounts crea el handler.
func NewAccounts(service AccountService) *Accounts { return &Accounts{service: service} }

// Routes monta las rutas, todas privadas.
func (h *Accounts) Routes(protected gin.IRoutes) {
	protected.GET("/accounts", h.list)
	protected.POST("/accounts", h.create)
	protected.PATCH("/accounts/:id", h.update)
	protected.POST("/accounts/:id/archive", h.archive)
}

type accountResponse struct {
	ID               uuid.UUID    `json:"id"`
	Name             string       `json:"name"`
	Type             string       `json:"type"`
	Currency         string       `json:"currency"`
	Balance          money.Amount `json:"balance"`
	IncludeInSavings bool         `json:"include_in_savings"`
}

type currencyAmountResponse struct {
	Currency string       `json:"currency"`
	Amount   money.Amount `json:"amount"`
}

type savingsResponse struct {
	IncludedCount int                      `json:"included_count"`
	ActiveCount   int                      `json:"active_count"`
	Totals        []currencyAmountResponse `json:"totals"`
}

type accountListResponse struct {
	Accounts []accountResponse `json:"accounts"`
	Savings  savingsResponse   `json:"savings"`
}

type createAccountRequest struct {
	ID               *string `json:"id"`
	Name             string  `json:"name"`
	Type             string  `json:"type"`
	Currency         string  `json:"currency"`
	InitialBalance   *string `json:"initial_balance"`
	IncludeInSavings *bool   `json:"include_in_savings"`
}

// La moneda y el saldo inicial se leen solo para rechazarlos: no cambian.
type updateAccountRequest struct {
	Name             *string         `json:"name"`
	Type             *string         `json:"type"`
	IncludeInSavings *bool           `json:"include_in_savings"`
	Currency         json.RawMessage `json:"currency"`
	InitialBalance   json.RawMessage `json:"initial_balance"`
}

func (h *Accounts) list(c *gin.Context) {
	list, err := h.service.List(c.Request.Context(), userID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	out := accountListResponse{
		Accounts: make([]accountResponse, 0, len(list.Accounts)),
		Savings:  toSavingsResponse(list.Savings),
	}
	for _, a := range list.Accounts {
		out.Accounts = append(out.Accounts, toAccountResponse(a))
	}
	apierror.OK(c, out)
}

func (h *Accounts) create(c *gin.Context) {
	var req createAccountRequest
	if !bind(c, &req) {
		return
	}
	in := app.CreateAccountInput{
		Name: req.Name, Type: domain.AccountType(req.Type), Currency: req.Currency,
		InitialBalance: req.InitialBalance, IncludeInSavings: req.IncludeInSavings,
	}
	if req.ID != nil {
		id, err := uuid.Parse(*req.ID)
		if err != nil {
			apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "id", Code: domain.CodeInvalidFormat}))
			return
		}
		in.ID = &id
	}
	account, created, err := h.service.Create(c.Request.Context(), userID(c), in)
	if err != nil {
		respondError(c, err)
		return
	}
	if created {
		apierror.Created(c, toAccountResponse(account))
		return
	}
	apierror.OK(c, toAccountResponse(account))
}

func (h *Accounts) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req updateAccountRequest
	if !bind(c, &req) {
		return
	}
	var fixed []apierror.FieldError
	if req.Currency != nil {
		fixed = append(fixed, apierror.FieldError{Field: "currency", Code: domain.CodeFieldNotEditable})
	}
	if req.InitialBalance != nil {
		fixed = append(fixed, apierror.FieldError{Field: "initial_balance", Code: domain.CodeFieldNotEditable})
	}
	if len(fixed) > 0 {
		apierror.Respond(c, apierror.Validation(fixed...))
		return
	}
	in := app.UpdateAccountInput{Name: req.Name, IncludeInSavings: req.IncludeInSavings}
	if req.Type != nil {
		t := domain.AccountType(*req.Type)
		in.Type = &t
	}
	account, err := h.service.Update(c.Request.Context(), userID(c), id, in)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toAccountResponse(account))
}

func (h *Accounts) archive(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.service.Archive(c.Request.Context(), userID(c), id); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

func toAccountResponse(a domain.Account) accountResponse {
	return accountResponse{
		ID: a.ID, Name: a.Name, Type: string(a.Type), Currency: a.Currency,
		Balance: money.NewAmount(a.Balance), IncludeInSavings: a.IncludeInSavings,
	}
}

// SavingsResponse es el ahorro total con la forma de GET /accounts (para Inicio).
func SavingsResponse(s domain.Savings) any { return toSavingsResponse(s) }

func toSavingsResponse(s domain.Savings) savingsResponse {
	out := savingsResponse{
		IncludedCount: s.IncludedCount,
		ActiveCount:   s.ActiveCount,
		Totals:        make([]currencyAmountResponse, 0, len(s.Totals)),
	}
	for _, t := range s.Totals {
		out.Totals = append(out.Totals, currencyAmountResponse{Currency: t.Currency, Amount: money.NewAmount(t.Amount)})
	}
	return out
}
