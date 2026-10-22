// Package http traduce HTTP ⇄ casos de uso de tarjetas y compras.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Service son los casos de uso que usa el handler.
type Service interface {
	ListCards(ctx context.Context, userID uuid.UUID) ([]domain.CardWithDebt, error)
	CreateCard(ctx context.Context, userID uuid.UUID, id *uuid.UUID, in app.CardInput) (domain.Card, bool, error)
	UpdateCard(ctx context.Context, userID, id uuid.UUID, in app.CardInput) (domain.Card, error)
	ArchiveCard(ctx context.Context, userID, id uuid.UUID) error
	ListPending(ctx context.Context, userID uuid.UUID) ([]app.PurchaseView, error)
	CreatePurchase(ctx context.Context, userID uuid.UUID, id *uuid.UUID, in app.PurchaseInput) (app.PurchaseView, bool, error)
	UpdatePurchase(ctx context.Context, userID, id uuid.UUID, in app.PurchaseInput) (app.PurchaseView, error)
	DeletePurchase(ctx context.Context, userID, id uuid.UUID) error
	Pay(ctx context.Context, userID, id uuid.UUID, in app.PayInput) (app.Payment, error)
}

var (
	errFxNotConfigured = apierror.New(http.StatusUnprocessableEntity, "FX_RATE_NOT_CONFIGURED",
		"Configura tu tipo de cambio en Ajustes para usar otra moneda.")
	errCardArchived = apierror.New(http.StatusUnprocessableEntity, "CARD_ARCHIVED",
		"Esta tarjeta está archivada. Elige otra.")
	errCategoryArchived = apierror.New(http.StatusUnprocessableEntity, "CATEGORY_ARCHIVED",
		"Esta categoría está archivada. Elige otra.")
	errCardHasPending = apierror.New(http.StatusConflict, "CARD_HAS_PENDING_PURCHASES",
		"Esta tarjeta tiene compras pendientes. Págalas o elimínalas antes de archivarla.")
	errPurchaseAlreadyPaid = apierror.New(http.StatusConflict, "PURCHASE_ALREADY_PAID",
		"Esta compra ya está pagada.")
	errAccountArchived = apierror.New(http.StatusUnprocessableEntity, "ACCOUNT_ARCHIVED",
		"Esta cuenta está archivada. Elige otra.")
)

// Handler atiende Tarjetas de Crédito y la compra con tarjeta.
type Handler struct {
	service Service
}

// NewHandler crea el handler.
func NewHandler(service Service) *Handler { return &Handler{service: service} }

// Routes monta las rutas, todas privadas.
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/credit-cards", h.listCards)
	protected.POST("/credit-cards", h.createCard)
	protected.PATCH("/credit-cards/:id", h.updateCard)
	protected.POST("/credit-cards/:id/archive", h.archiveCard)
	protected.GET("/credit-purchases", h.listPurchases)
	protected.POST("/credit-purchases", h.createPurchase)
	protected.PATCH("/credit-purchases/:id", h.updatePurchase)
	protected.DELETE("/credit-purchases/:id", h.deletePurchase)
	protected.POST("/credit-purchases/:id/pay", h.pay)
}

type amountResponse struct {
	Currency string       `json:"currency"`
	Amount   money.Amount `json:"amount"`
}

type cardResponse struct {
	ID           uuid.UUID        `json:"id"`
	Name         string           `json:"name"`
	Currency     string           `json:"currency"`
	Debt         []amountResponse `json:"debt"`
	PendingCount int              `json:"pending_count"`
	NextDueDate  *dates.LocalDate `json:"next_due_date"`
}

type cardRefResponse struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Currency string    `json:"currency"`
}

type categoryResponse struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Kind  string    `json:"kind"`
	Color string    `json:"color"`
}

type purchaseResponse struct {
	ID           uuid.UUID        `json:"id"`
	CardID       uuid.UUID        `json:"card_id"`
	Card         cardRefResponse  `json:"card"`
	Description  string           `json:"description"`
	Amount       money.Amount     `json:"amount"`
	Currency     string           `json:"currency"`
	PurchaseDate dates.LocalDate  `json:"purchase_date"`
	DueDate      dates.LocalDate  `json:"due_date"`
	Status       string           `json:"status"`
	Category     categoryResponse `json:"category"`
}

type cardRequest struct {
	ID       *string `json:"id"`
	Name     *string `json:"name"`
	Currency *string `json:"currency"`
}

type purchaseRequest struct {
	ID          *string `json:"id"`
	CardID      *string `json:"card_id"`
	CategoryID  *string `json:"category_id"`
	Amount      *string `json:"amount"`
	Description *string `json:"description"`
	DueDate     *string `json:"due_date"`
}

func (h *Handler) listCards(c *gin.Context) {
	cards, err := h.service.ListCards(c.Request.Context(), userID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	out := make([]cardResponse, 0, len(cards))
	for _, card := range cards {
		out = append(out, toCardResponse(card))
	}
	apierror.OK(c, out)
}

func (h *Handler) createCard(c *gin.Context) {
	var req cardRequest
	if !bind(c, &req) {
		return
	}
	ids, ok := parseIDs(c, idField{"id", req.ID})
	if !ok {
		return
	}
	card, created, err := h.service.CreateCard(c.Request.Context(), userID(c), ids["id"], app.CardInput{Name: req.Name, Currency: req.Currency})
	if err != nil {
		respondError(c, err)
		return
	}
	h.respondCard(c, card.ID, created)
}

func (h *Handler) updateCard(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req cardRequest
	if !bind(c, &req) {
		return
	}
	if _, err := h.service.UpdateCard(c.Request.Context(), userID(c), id, app.CardInput{Name: req.Name, Currency: req.Currency}); err != nil {
		respondError(c, err)
		return
	}
	h.respondCard(c, id, false)
}

// respondCard responde la tarjeta con su deuda, como en la lista.
func (h *Handler) respondCard(c *gin.Context, id uuid.UUID, created bool) {
	cards, err := h.service.ListCards(c.Request.Context(), userID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	for _, card := range cards {
		if card.ID != id {
			continue
		}
		if created {
			apierror.Created(c, toCardResponse(card))
		} else {
			apierror.OK(c, toCardResponse(card))
		}
		return
	}
	apierror.Respond(c, apierror.ErrNotFound)
}

func (h *Handler) archiveCard(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.service.ArchiveCard(c.Request.Context(), userID(c), id); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

func (h *Handler) listPurchases(c *gin.Context) {
	if status := c.DefaultQuery("status", domain.StatusPending); status != domain.StatusPending {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "status", Code: domain.CodeInvalidFormat}))
		return
	}
	purchases, err := h.service.ListPending(c.Request.Context(), userID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	out := make([]purchaseResponse, 0, len(purchases))
	for _, p := range purchases {
		out = append(out, toPurchaseResponse(p))
	}
	apierror.OK(c, out)
}

func (h *Handler) createPurchase(c *gin.Context) {
	var req purchaseRequest
	if !bind(c, &req) {
		return
	}
	ids, ok := parseIDs(c, idField{"id", req.ID}, idField{"card_id", req.CardID}, idField{"category_id", req.CategoryID})
	if !ok {
		return
	}
	purchase, created, err := h.service.CreatePurchase(c.Request.Context(), userID(c), ids["id"], app.PurchaseInput{
		CardID: ids["card_id"], CategoryID: ids["category_id"], Amount: req.Amount, Description: req.Description, DueDate: req.DueDate,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	if created {
		apierror.Created(c, toPurchaseResponse(purchase))
		return
	}
	apierror.OK(c, toPurchaseResponse(purchase))
}

func (h *Handler) updatePurchase(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req purchaseRequest
	if !bind(c, &req) {
		return
	}
	ids, ok := parseIDs(c, idField{"card_id", req.CardID}, idField{"category_id", req.CategoryID})
	if !ok {
		return
	}
	purchase, err := h.service.UpdatePurchase(c.Request.Context(), userID(c), id, app.PurchaseInput{
		CardID: ids["card_id"], CategoryID: ids["category_id"], Amount: req.Amount, Description: req.Description, DueDate: req.DueDate,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toPurchaseResponse(purchase))
}

func (h *Handler) deletePurchase(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	// Una ya eliminada responde 204; una que nunca existió o es de otro, 404.
	if err := h.service.DeletePurchase(c.Request.Context(), userID(c), id); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

func toCardResponse(c domain.CardWithDebt) cardResponse {
	debt := make([]amountResponse, 0, len(c.Debt))
	for _, d := range c.Debt {
		debt = append(debt, amountResponse{Currency: d.Currency, Amount: money.NewAmount(d.Amount)})
	}
	return cardResponse{ID: c.ID, Name: c.Name, Currency: c.Currency, Debt: debt, PendingCount: c.PendingCount, NextDueDate: c.NextDueDate}
}

func toPurchaseResponse(p app.PurchaseView) purchaseResponse {
	return purchaseResponse{
		ID:           p.ID,
		CardID:       p.CardID,
		Card:         cardRefResponse{ID: p.Card.ID, Name: p.Card.Name, Currency: p.Card.Currency},
		Description:  p.Description,
		Amount:       money.NewAmount(p.Amount),
		Currency:     p.Currency,
		PurchaseDate: p.PurchaseDate,
		DueDate:      p.DueDate,
		Status:       p.Status,
		Category:     categoryResponse{ID: p.Category.ID, Name: p.Category.Name, Kind: p.Category.Kind, Color: p.Category.Color},
	}
}

func userID(c *gin.Context) uuid.UUID {
	principal, _ := auth.FromContext(c.Request.Context())
	return principal.UserID
}

// pathID lee el id de la ruta. Uno que no es UUID no puede existir: 404.
func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.UUID{}, false
	}
	return id, true
}

func bind(c *gin.Context, target any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierror.Respond(c, err)
			return false
		}
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "body", Code: domain.CodeInvalidFormat}))
		return false
	}
	return true
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

func respondError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		fields := make([]apierror.FieldError, 0, len(validation.Fields))
		for _, f := range validation.Fields {
			fields = append(fields, apierror.FieldError{Field: f.Field, Code: f.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.Is(err, domain.ErrNotFound):
		apierror.Respond(c, apierror.ErrNotFound)
	case errors.Is(err, domain.ErrIDTaken):
		apierror.Respond(c, apierror.ErrIDConflict)
	case errors.Is(err, domain.ErrFxNotConfigured):
		apierror.Respond(c, errFxNotConfigured)
	case errors.Is(err, domain.ErrCardArchived):
		apierror.Respond(c, errCardArchived)
	case errors.Is(err, domain.ErrCategoryArchived):
		apierror.Respond(c, errCategoryArchived)
	case errors.Is(err, domain.ErrCardHasPending):
		apierror.Respond(c, errCardHasPending)
	case errors.Is(err, domain.ErrPurchaseAlreadyPaid):
		apierror.Respond(c, errPurchaseAlreadyPaid)
	case errors.Is(err, domain.ErrAccountArchived):
		apierror.Respond(c, errAccountArchived)
	default:
		apierror.Respond(c, err)
	}
}

type payRequest struct {
	AccountID  *string `json:"account_id"`
	PaidAmount *string `json:"paid_amount"`
	PaidOn     *string `json:"paid_on"`
}

type paymentResponse struct {
	Purchase    purchaseResponse `json:"purchase"`
	Transaction any              `json:"transaction"`
}

func (h *Handler) pay(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req payRequest
	if !bind(c, &req) {
		return
	}
	ids, ok := parseIDs(c, idField{"account_id", req.AccountID})
	if !ok {
		return
	}
	payment, err := h.service.Pay(c.Request.Context(), userID(c), id, app.PayInput{
		AccountID: ids["account_id"], PaidAmount: req.PaidAmount, PaidOn: req.PaidOn,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, paymentResponse{Purchase: toPurchaseResponse(payment.Purchase), Transaction: payment.Transaction})
}
