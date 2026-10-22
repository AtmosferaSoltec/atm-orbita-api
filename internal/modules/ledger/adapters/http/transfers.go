package http

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// TransferService son los casos de uso de transferencias que usa el handler.
type TransferService interface {
	Create(ctx context.Context, userID uuid.UUID, id *uuid.UUID, in app.TransferInput) (domain.TransferEntry, bool, error)
	Update(ctx context.Context, userID, id uuid.UUID, in app.TransferInput) (domain.TransferEntry, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

// Transfers atiende Transferir / Editar transferencia.
type Transfers struct {
	service TransferService
}

// NewTransfers crea el handler.
func NewTransfers(service TransferService) *Transfers { return &Transfers{service: service} }

// Routes monta las rutas, todas privadas.
func (h *Transfers) Routes(protected gin.IRoutes) {
	protected.POST("/transfers", h.create)
	protected.PATCH("/transfers/:id", h.update)
	protected.DELETE("/transfers/:id", h.delete)
}

// transferResponse es una transferencia, en la lista y al guardarla.
type transferResponse struct {
	EntryType    string               `json:"entry_type"`
	ID           uuid.UUID            `json:"id"`
	OccurredOn   dates.LocalDate      `json:"occurred_on"`
	Note         string               `json:"note"`
	FromAccount  entryAccountResponse `json:"from_account"`
	ToAccount    entryAccountResponse `json:"to_account"`
	FromAmount   money.Amount         `json:"from_amount"`
	ToAmount     money.Amount         `json:"to_amount"`
	ExchangeRate *money.Rate          `json:"exchange_rate"`
}

// En un PATCH, exchange_rate distingue "no vino" de null (misma moneda).
type transferRequest struct {
	ID            *string         `json:"id"`
	FromAccountID *string         `json:"from_account_id"`
	ToAccountID   *string         `json:"to_account_id"`
	FromAmount    *string         `json:"from_amount"`
	ToAmount      *string         `json:"to_amount"`
	ExchangeRate  json.RawMessage `json:"exchange_rate"`
	OccurredOn    *string         `json:"occurred_on"`
	Note          *string         `json:"note"`
}

func (h *Transfers) create(c *gin.Context) {
	var req transferRequest
	if !bind(c, &req) {
		return
	}
	in, ids, ok := toTransferInput(c, req, idField{"id", req.ID})
	if !ok {
		return
	}
	entry, created, err := h.service.Create(c.Request.Context(), userID(c), ids["id"], in)
	if err != nil {
		respondError(c, err)
		return
	}
	if created {
		apierror.Created(c, toTransferResponse(entry))
		return
	}
	apierror.OK(c, toTransferResponse(entry))
}

func (h *Transfers) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req transferRequest
	if !bind(c, &req) {
		return
	}
	in, _, ok := toTransferInput(c, req)
	if !ok {
		return
	}
	entry, err := h.service.Update(c.Request.Context(), userID(c), id, in)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toTransferResponse(entry))
}

func (h *Transfers) delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), userID(c), id); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

// toTransferInput lee los UUID y el tipo de cambio (texto o null).
func toTransferInput(c *gin.Context, req transferRequest, extra ...idField) (app.TransferInput, map[string]*uuid.UUID, bool) {
	fields := append(extra, idField{"from_account_id", req.FromAccountID}, idField{"to_account_id", req.ToAccountID})
	ids, ok := parseIDs(c, fields...)
	if !ok {
		return app.TransferInput{}, nil, false
	}
	in := app.TransferInput{
		FromAccountID: ids["from_account_id"], ToAccountID: ids["to_account_id"],
		FromAmount: req.FromAmount, ToAmount: req.ToAmount, Note: req.Note, OccurredOn: req.OccurredOn,
	}
	if req.ExchangeRate != nil {
		in.ExchangeRate.Set = true
		var rate *string
		if err := json.Unmarshal(req.ExchangeRate, &rate); err != nil {
			apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "exchange_rate", Code: domain.CodeInvalidFormat}))
			return app.TransferInput{}, nil, false
		}
		in.ExchangeRate.Value = rate
	}
	return in, ids, true
}

func toTransferResponse(e domain.TransferEntry) transferResponse {
	out := transferResponse{
		EntryType:   "transfer",
		ID:          e.ID,
		OccurredOn:  e.OccurredOn,
		Note:        e.Note,
		FromAccount: toEntryAccountResponse(e.From),
		ToAccount:   toEntryAccountResponse(e.To),
		FromAmount:  money.NewAmount(e.FromAmount),
		ToAmount:    money.NewAmount(e.ToAmount),
	}
	if e.ExchangeRate != nil {
		rate := money.NewRate(*e.ExchangeRate)
		out.ExchangeRate = &rate
	}
	return out
}

func toEntryAccountResponse(a domain.EntryAccount) entryAccountResponse {
	return entryAccountResponse{ID: a.ID, Name: a.Name, Type: string(a.Type), Currency: a.Currency}
}
