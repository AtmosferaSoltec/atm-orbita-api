// Package http exposes private debtor operations and revocable read-only sharing.
package http

import (
	"encoding/json"
	"errors"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"io"
	"net/http"
)

type Handler struct{ service *app.Service }

func New(s *app.Service) *Handler { return &Handler{s} }
func (h *Handler) Routes(public, protected gin.IRoutes) {
	protected.GET("/loan-debtors", h.list)
	protected.POST("/loan-debtors", h.create)
	protected.GET("/loan-debtors/:id", h.get)
	protected.PATCH("/loan-debtors/:id", h.rename)
	protected.DELETE("/loan-debtors/:id", h.remove)
	protected.POST("/loan-debtors/:id/movements", h.createMovement)
	protected.PATCH("/loan-debtors/:id/movements/:movement_id", h.updateMovement)
	protected.DELETE("/loan-debtors/:id/movements/:movement_id", h.removeMovement)
	protected.PUT("/loan-debtors/:id/movements/:movement_id/plan", h.plan)
	protected.PUT("/loan-debtors/:id/share", h.share)
	protected.DELETE("/loan-debtors/:id/share", h.revoke)
	public.GET("/shared/loan-debtors/:token", h.shared)
}

type debtorBody struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}
type planBody struct {
	Month  string `json:"month"`
	Amount string `json:"amount"`
}
type movementBody struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Amount       string     `json:"amount"`
	Date         string     `json:"date"`
	AppliedMonth *string    `json:"applied_month"`
	Title        string     `json:"title"`
	Note         string     `json:"note"`
	Plan         []planBody `json:"plan"`
}

func parseID(raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, domain.Invalid("id", "INVALID_FORMAT")
	}
	return id, nil
}
func parsePlan(raw []planBody) ([]domain.Plan, error) {
	result := make([]domain.Plan, 0, len(raw))
	for _, p := range raw {
		amount, err := money.ParseAmount(p.Amount)
		if err != nil {
			return nil, domain.Invalid("plan.amount", "AMOUNT_INVALID")
		}
		result = append(result, domain.Plan{Month: p.Month, Amount: amount})
	}
	return result, nil
}
func (b movementBody) parse() (domain.Movement, error) {
	id, err := parseID(b.ID)
	if err != nil {
		return domain.Movement{}, err
	}
	amount, err := money.ParseAmount(b.Amount)
	if err != nil {
		return domain.Movement{}, domain.Invalid("amount", "AMOUNT_INVALID")
	}
	date, err := dates.Parse(b.Date)
	if err != nil {
		return domain.Movement{}, domain.Invalid("date", "LOAN_DATE_INVALID")
	}
	plan, err := parsePlan(b.Plan)
	if err != nil {
		return domain.Movement{}, err
	}
	month := ""
	if b.AppliedMonth != nil {
		month = *b.AppliedMonth
	}
	return domain.Movement{ID: id, Kind: b.Kind, Amount: amount, Date: date, AppliedMonth: month, Title: b.Title, Note: b.Note, Plan: plan}, nil
}
func userID(c *gin.Context) uuid.UUID { p, _ := auth.FromContext(c.Request.Context()); return p.UserID }
func pathID(c *gin.Context, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(key))
	if err != nil || id == uuid.Nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}
func bind(c *gin.Context, body any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(body)
	if err == nil {
		var extra any
		if tail := decoder.Decode(&extra); !errors.Is(tail, io.EOF) {
			err = errors.New("multiple JSON values")
		}
	}
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			apierror.Respond(c, err)
		} else {
			apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "body", Code: "INVALID_FORMAT"}))
		}
		return false
	}
	return true
}
func respond(c *gin.Context, err error) {
	var v *domain.ValidationError
	switch {
	case errors.As(err, &v):
		fields := make([]apierror.FieldError, 0, len(v.Fields))
		for _, f := range v.Fields {
			fields = append(fields, apierror.FieldError{Field: f.Field, Code: f.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.Is(err, domain.ErrNotFound):
		apierror.Respond(c, apierror.ErrNotFound)
	case errors.Is(err, domain.ErrIDConflict):
		apierror.Respond(c, apierror.ErrIDConflict)
	case errors.Is(err, domain.ErrBalance):
		apierror.Respond(c, apierror.New(409, "LOAN_BALANCE_EXCEEDED", "Los abonos no pueden superar lo prestado en esa fecha."))
	case errors.Is(err, domain.ErrCurrencyLocked):
		apierror.Respond(c, apierror.New(409, "DEBTOR_CURRENCY_LOCKED", "La moneda del deudor no se puede cambiar."))
	case errors.Is(err, domain.ErrKindLocked):
		apierror.Respond(c, apierror.New(409, "LOAN_KIND_LOCKED", "No se puede cambiar el tipo de movimiento."))
	default:
		apierror.Respond(c, err)
	}
}
func output(d domain.Debtor, private bool) gin.H {
	movements := make([]gin.H, 0, len(d.Movements))
	for _, m := range d.Movements {
		plan := make([]gin.H, 0, len(m.Plan))
		for _, p := range m.Plan {
			plan = append(plan, gin.H{"month": p.Month, "amount": p.Amount})
		}
		var month *string
		if m.AppliedMonth != "" {
			value := m.AppliedMonth
			month = &value
		}
		movements = append(movements, gin.H{"id": m.ID, "kind": m.Kind, "amount": m.Amount, "date": m.Date, "applied_month": month, "title": m.Title, "note": m.Note, "plan": plan})
	}
	out := gin.H{"id": d.ID, "name": d.Name, "currency": d.Currency, "movements": movements, "created_at": d.CreatedAt}
	if private {
		out["share_token"] = d.ShareToken
	}
	return out
}
func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex, nofollow")
}
func (h *Handler) list(c *gin.Context) {
	noStore(c)
	items, today, err := h.service.List(c.Request.Context(), userID(c))
	if err != nil {
		respond(c, err)
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, d := range items {
		out = append(out, output(d, true))
	}
	c.JSON(200, gin.H{"data": out, "meta": gin.H{"today": today}})
}
func (h *Handler) get(c *gin.Context) {
	noStore(c)
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	d, err := h.service.Get(c.Request.Context(), userID(c), id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(d, true))
}
func (h *Handler) create(c *gin.Context) {
	noStore(c)
	var b debtorBody
	if !bind(c, &b) {
		return
	}
	id, err := parseID(b.ID)
	if err != nil {
		respond(c, err)
		return
	}
	d, created, err := h.service.Create(c.Request.Context(), userID(c), domain.Debtor{ID: id, Name: b.Name, Currency: b.Currency})
	if err != nil {
		respond(c, err)
		return
	}
	if created {
		apierror.Created(c, output(d, true))
	} else {
		apierror.OK(c, output(d, true))
	}
}
func (h *Handler) rename(c *gin.Context) {
	noStore(c)
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var b debtorBody
	if !bind(c, &b) {
		return
	}
	if b.ID != "" && b.ID != id.String() {
		respond(c, domain.Invalid("id", "INVALID_FORMAT"))
		return
	}
	d, err := h.service.Rename(c.Request.Context(), userID(c), id, b.Name, b.Currency)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(d, true))
}
func (h *Handler) remove(c *gin.Context) {
	noStore(c)
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), userID(c), id); err != nil {
		respond(c, err)
		return
	}
	apierror.NoContent(c)
}
func (h *Handler) createMovement(c *gin.Context) { h.saveMovement(c, false) }
func (h *Handler) updateMovement(c *gin.Context) { h.saveMovement(c, true) }
func (h *Handler) saveMovement(c *gin.Context, updating bool) {
	noStore(c)
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var b movementBody
	if !bind(c, &b) {
		return
	}
	if updating {
		mid, ok := pathID(c, "movement_id")
		if !ok {
			return
		}
		if b.ID != "" && b.ID != mid.String() {
			respond(c, domain.Invalid("id", "INVALID_FORMAT"))
			return
		}
		b.ID = mid.String()
	}
	m, err := b.parse()
	if err != nil {
		respond(c, err)
		return
	}
	d, created, err := h.service.SaveMovement(c.Request.Context(), userID(c), id, m, updating)
	if err != nil {
		respond(c, err)
		return
	}
	if created {
		apierror.Created(c, output(d, true))
	} else {
		apierror.OK(c, output(d, true))
	}
}
func (h *Handler) removeMovement(c *gin.Context) {
	noStore(c)
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	mid, ok := pathID(c, "movement_id")
	if !ok {
		return
	}
	d, err := h.service.RemoveMovement(c.Request.Context(), userID(c), id, mid)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(d, true))
}
func (h *Handler) plan(c *gin.Context) {
	noStore(c)
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	mid, ok := pathID(c, "movement_id")
	if !ok {
		return
	}
	var b struct {
		Plan []planBody `json:"plan"`
	}
	if !bind(c, &b) {
		return
	}
	plan, err := parsePlan(b.Plan)
	if err != nil {
		respond(c, err)
		return
	}
	d, err := h.service.Plan(c.Request.Context(), userID(c), id, mid, plan)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(d, true))
}
func (h *Handler) share(c *gin.Context)  { h.setShare(c, true) }
func (h *Handler) revoke(c *gin.Context) { h.setShare(c, false) }
func (h *Handler) setShare(c *gin.Context, enabled bool) {
	noStore(c)
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	d, err := h.service.Share(c.Request.Context(), userID(c), id, enabled)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(d, true))
}
func (h *Handler) shared(c *gin.Context) {
	noStore(c)
	d, err := h.service.Shared(c.Request.Context(), c.Param("token"))
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(d, false))
}
