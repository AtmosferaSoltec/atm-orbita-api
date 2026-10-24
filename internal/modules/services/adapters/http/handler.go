// Package http translates subscription requests and stable validation errors.
package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

type Handler struct{ service *app.Service }

func New(s *app.Service) *Handler { return &Handler{s} }
func (h *Handler) Routes(r gin.IRoutes) {
	r.GET("/services", h.list)
	r.POST("/services", h.create)
	r.GET("/services/:id", h.get)
	r.PATCH("/services/:id", h.update)
	r.DELETE("/services/:id", h.remove)
	r.PUT("/services/:id/payments", h.markPaid)
	r.DELETE("/services/:id/members/:member_id/payments/:month", h.unmarkPaid)
}

type memberBody struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Amount string `json:"amount"`
	Since  string `json:"since"`
}
type serviceBody struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Color      string       `json:"color"`
	Currency   string       `json:"currency"`
	Amount     string       `json:"amount"`
	BillingDay int          `json:"billing_day"`
	StartedOn  string       `json:"started_on"`
	Status     string       `json:"status"`
	EndedOn    *string      `json:"ended_on"`
	Note       string       `json:"note"`
	Shared     bool         `json:"shared"`
	Members    []memberBody `json:"members"`
}

func (b serviceBody) parse() (domain.Service, error) {
	var v domain.ValidationError
	amount, err := money.ParseAmount(b.Amount)
	if err != nil {
		v.Add("amount", "AMOUNT_INVALID")
	}
	start, err := dates.Parse(b.StartedOn)
	if err != nil {
		v.Add("started_on", "SERVICE_DATE_INVALID")
	}
	s := domain.Service{Name: b.Name, Color: b.Color, Currency: b.Currency, Amount: amount, BillingDay: b.BillingDay, StartedOn: start, Status: b.Status, Note: b.Note, Shared: b.Shared, Members: []domain.Member{}}
	if b.ID != "" {
		s.ID, err = uuid.Parse(b.ID)
		if err != nil {
			v.Add("id", "INVALID_FORMAT")
		}
	}
	if b.EndedOn != nil && b.Status == "cancelled" {
		d, err := dates.Parse(*b.EndedOn)
		if err != nil {
			v.Add("ended_on", "SERVICE_END_INVALID")
		} else {
			s.EndedOn = &d
		}
	}
	if b.Shared {
		for _, m := range b.Members {
			id, err := uuid.Parse(m.ID)
			if err != nil {
				v.Add("members.id", "SERVICE_MEMBER_INVALID")
			}
			amount, err := money.ParseAmount(m.Amount)
			if err != nil {
				v.Add("members.amount", "AMOUNT_INVALID")
			}
			s.Members = append(s.Members, domain.Member{ID: id, Name: m.Name, Amount: amount, Since: m.Since})
		}
	}
	return s, v.OrNil()
}

func userID(c *gin.Context) uuid.UUID { p, _ := auth.FromContext(c.Request.Context()); return p.UserID }
func pathID(c *gin.Context, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(key))
	if err != nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func bind(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
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
	case errors.Is(err, domain.ErrCurrencyLocked):
		apierror.Respond(c, apierror.New(http.StatusConflict, "SERVICE_CURRENCY_LOCKED", "La moneda no se puede cambiar mientras existan pagos registrados."))
	default:
		apierror.Respond(c, err)
	}
}

func output(s domain.Service) gin.H {
	members := make([]gin.H, 0, len(s.Members))
	for _, m := range s.Members {
		members = append(members, gin.H{"id": m.ID, "name": m.Name, "amount": m.Amount, "since": m.Since})
	}
	payments := make([]gin.H, 0, len(s.Payments))
	for _, p := range s.Payments {
		payments = append(payments, gin.H{"id": p.ID, "member_id": p.MemberID, "month": p.Month, "paid_on": p.PaidOn, "amount": p.Amount})
	}
	return gin.H{"id": s.ID, "name": s.Name, "color": s.Color, "currency": s.Currency, "amount": s.Amount, "billing_day": s.BillingDay, "started_on": s.StartedOn, "status": s.Status, "ended_on": s.EndedOn, "note": s.Note, "shared": s.Shared, "members": members, "member_payments": payments, "created_at": s.CreatedAt}
}

func (h *Handler) list(c *gin.Context) {
	items, today, err := h.service.List(c.Request.Context(), userID(c))
	if err != nil {
		respond(c, err)
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, s := range items {
		out = append(out, output(s))
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "meta": gin.H{"today": today}})
}

func (h *Handler) get(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	s, err := h.service.Get(c.Request.Context(), userID(c), id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(s))
}

func (h *Handler) create(c *gin.Context) {
	var body serviceBody
	if !bind(c, &body) {
		return
	}
	in, err := body.parse()
	if err != nil {
		respond(c, err)
		return
	}
	s, created, err := h.service.Create(c.Request.Context(), userID(c), in)
	if err != nil {
		respond(c, err)
		return
	}
	if created {
		apierror.Created(c, output(s))
	} else {
		apierror.OK(c, output(s))
	}
}

func (h *Handler) update(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var body serviceBody
	if !bind(c, &body) {
		return
	}
	in, err := body.parse()
	if err != nil {
		respond(c, err)
		return
	}
	s, err := h.service.Update(c.Request.Context(), userID(c), id, in)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(s))
}

func (h *Handler) remove(c *gin.Context) {
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

func (h *Handler) markPaid(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Payments []struct {
			MemberID string `json:"member_id"`
			Month    string `json:"month"`
			PaidOn   string `json:"paid_on"`
		} `json:"payments"`
	}
	if !bind(c, &body) {
		return
	}
	payments := make([]domain.Payment, 0, len(body.Payments))
	var v domain.ValidationError
	for _, p := range body.Payments {
		member, err := uuid.Parse(p.MemberID)
		if err != nil {
			v.Add("member_id", "SERVICE_MEMBER_INVALID")
		}
		paid, err := dates.Parse(p.PaidOn)
		if err != nil {
			v.Add("paid_on", "SERVICE_PAYMENT_DATE_INVALID")
		}
		payments = append(payments, domain.Payment{MemberID: member, Month: p.Month, PaidOn: paid})
	}
	if err := v.OrNil(); err != nil {
		respond(c, err)
		return
	}
	s, err := h.service.MarkPaid(c.Request.Context(), userID(c), id, payments)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(s))
}

func (h *Handler) unmarkPaid(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	member, ok := pathID(c, "member_id")
	if !ok {
		return
	}
	s, err := h.service.UnmarkPaid(c.Request.Context(), userID(c), id, member, c.Param("month"))
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, output(s))
}
