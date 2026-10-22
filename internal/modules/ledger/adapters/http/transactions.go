package http

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// TransactionService son los casos de uso de movimientos que usa el handler.
type TransactionService interface {
	Create(ctx context.Context, userID uuid.UUID, in app.CreateTransactionInput) (domain.Entry, bool, error)
	Update(ctx context.Context, userID, id uuid.UUID, in app.UpdateTransactionInput) (domain.Entry, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
	ListEntries(ctx context.Context, userID uuid.UUID, q app.EntryQuery) (app.EntryPage, error)
}

var (
	errAccountArchived = apierror.New(http.StatusUnprocessableEntity, "ACCOUNT_ARCHIVED",
		"Esta cuenta está archivada. Elige otra.")
	errCategoryArchived = apierror.New(http.StatusUnprocessableEntity, "CATEGORY_ARCHIVED",
		"Esta categoría está archivada. Elige otra.")
)

// Transactions atiende Nuevo/Editar movimiento y la lista de movimientos.
type Transactions struct {
	service TransactionService
}

// NewTransactions crea el handler.
func NewTransactions(service TransactionService) *Transactions {
	return &Transactions{service: service}
}

// Routes monta las rutas, todas privadas.
func (h *Transactions) Routes(protected gin.IRoutes) {
	protected.POST("/transactions", h.create)
	protected.PATCH("/transactions/:id", h.update)
	protected.DELETE("/transactions/:id", h.delete)
	protected.GET("/entries", h.list)
}

type entryAccountResponse struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Currency string    `json:"currency"`
}

// entryResponse es una fila de la lista. entry_type distingue los movimientos
// de las transferencias (plan 06); credit_purchase_id llega con el plan 10.
type entryResponse struct {
	EntryType        string               `json:"entry_type"`
	ID               uuid.UUID            `json:"id"`
	Kind             string               `json:"kind"`
	OccurredOn       dates.LocalDate      `json:"occurred_on"`
	Amount           money.Amount         `json:"amount"`
	Currency         string               `json:"currency"`
	Description      string               `json:"description"`
	Account          entryAccountResponse `json:"account"`
	Category         categoryResponse     `json:"category"`
	CreditPurchaseID *uuid.UUID           `json:"credit_purchase_id"`
}

type createTransactionRequest struct {
	ID          *string `json:"id"`
	AccountID   *string `json:"account_id"`
	CategoryID  *string `json:"category_id"`
	Amount      *string `json:"amount"`
	Description string  `json:"description"`
	OccurredOn  *string `json:"occurred_on"`
}

type updateTransactionRequest struct {
	AccountID   *string `json:"account_id"`
	CategoryID  *string `json:"category_id"`
	Amount      *string `json:"amount"`
	Description *string `json:"description"`
	OccurredOn  *string `json:"occurred_on"`
}

func (h *Transactions) create(c *gin.Context) {
	var req createTransactionRequest
	if !bind(c, &req) {
		return
	}
	ids, ok := parseIDs(c, idField{"id", req.ID}, idField{"account_id", req.AccountID}, idField{"category_id", req.CategoryID})
	if !ok {
		return
	}
	entry, created, err := h.service.Create(c.Request.Context(), userID(c), app.CreateTransactionInput{
		ID: ids["id"], AccountID: ids["account_id"], CategoryID: ids["category_id"],
		Amount: req.Amount, Description: req.Description, OccurredOn: req.OccurredOn,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	if created {
		apierror.Created(c, toEntryResponse(entry))
		return
	}
	apierror.OK(c, toEntryResponse(entry))
}

func (h *Transactions) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req updateTransactionRequest
	if !bind(c, &req) {
		return
	}
	ids, ok := parseIDs(c, idField{"account_id", req.AccountID}, idField{"category_id", req.CategoryID})
	if !ok {
		return
	}
	entry, err := h.service.Update(c.Request.Context(), userID(c), id, app.UpdateTransactionInput{
		AccountID: ids["account_id"], CategoryID: ids["category_id"],
		Amount: req.Amount, Description: req.Description, OccurredOn: req.OccurredOn,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toEntryResponse(entry))
}

func (h *Transactions) delete(c *gin.Context) {
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

func (h *Transactions) list(c *gin.Context) {
	q := app.EntryQuery{From: c.Query("from"), To: c.Query("to"), Q: c.Query("q"), Page: 1, PageSize: app.DefaultPageSize}
	var problems []apierror.FieldError
	if text, ok := c.GetQuery("account_id"); ok {
		id, err := uuid.Parse(text)
		if err != nil {
			problems = append(problems, apierror.FieldError{Field: "account_id", Code: domain.CodeInvalidFormat})
		}
		q.AccountID = &id
	}
	for _, p := range []struct {
		field  string
		target *int
	}{{"page", &q.Page}, {"page_size", &q.PageSize}} {
		field, target := p.field, p.target
		if text, ok := c.GetQuery(field); ok {
			n, err := strconv.Atoi(text)
			if err != nil {
				problems = append(problems, apierror.FieldError{Field: field, Code: domain.CodeInvalidFormat})
			}
			*target = n
		}
	}
	if len(problems) > 0 {
		apierror.Respond(c, apierror.Validation(problems...))
		return
	}
	page, err := h.service.ListEntries(c.Request.Context(), userID(c), q)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.Page(c, EntryResponses(page.Items), apierror.Meta{Page: page.Page, PageSize: page.PageSize, Total: page.Total})
}

// idField es un UUID opcional del cuerpo, con el nombre de su campo.
type idField struct {
	name  string
	value *string
}

// parseIDs lee los UUID opcionales del cuerpo, en orden; uno mal formado es
// INVALID_FORMAT.
func parseIDs(c *gin.Context, fields ...idField) (map[string]*uuid.UUID, bool) {
	out := make(map[string]*uuid.UUID, len(fields))
	var problems []apierror.FieldError
	for _, field := range fields {
		if field.value == nil {
			continue
		}
		id, err := uuid.Parse(*field.value)
		if err != nil {
			problems = append(problems, apierror.FieldError{Field: field.name, Code: domain.CodeInvalidFormat})
			continue
		}
		out[field.name] = &id
	}
	if len(problems) > 0 {
		apierror.Respond(c, apierror.Validation(problems...))
		return nil, false
	}
	return out, true
}

func toEntryResponse(e domain.Entry) entryResponse {
	return entryResponse{
		EntryType:   "transaction",
		ID:          e.ID,
		Kind:        string(e.Kind),
		OccurredOn:  e.OccurredOn,
		Amount:      money.NewAmount(e.Amount),
		Currency:    e.Account.Currency,
		Description: e.Description,
		Account: entryAccountResponse{
			ID: e.Account.ID, Name: e.Account.Name, Type: string(e.Account.Type), Currency: e.Account.Currency,
		},
		Category: toResponse(e.Category),
	}
}

// EntryResponses son filas de la lista con la forma de GET /entries (para Inicio).
func EntryResponses(entries []domain.ListEntry) []any {
	items := make([]any, 0, len(entries))
	for _, e := range entries {
		if e.Transfer != nil {
			items = append(items, toTransferResponse(*e.Transfer))
		} else {
			items = append(items, toEntryResponse(*e.Movement))
		}
	}
	return items
}
