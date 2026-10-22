// Package http traduce HTTP ⇄ casos de uso del perfil.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/currency"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Service son los casos de uso que usa el handler.
type Service interface {
	Get(ctx context.Context, userID uuid.UUID) (app.Settings, error)
	SetFx(ctx context.Context, userID uuid.UUID, in app.SetFxInput) (app.Settings, error)
	Update(ctx context.Context, userID uuid.UUID, in app.UpdateInput) (app.Settings, error)
}

const codeFxNotConfigured = "FX_RATE_NOT_CONFIGURED"

var errFxNotConfigured = apierror.New(http.StatusUnprocessableEntity, codeFxNotConfigured,
	"Configura tu tipo de cambio en Ajustes para usar otra moneda.")

// Handler atiende Ajustes y el catálogo de monedas.
type Handler struct {
	service Service
}

// NewHandler crea el handler.
func NewHandler(service Service) *Handler { return &Handler{service: service} }

// Routes monta las rutas: el catálogo es público; los ajustes, privados.
func (h *Handler) Routes(public, protected gin.IRoutes) {
	public.GET("/currencies", h.currencies)
	protected.GET("/settings", h.get)
	protected.PUT("/settings/fx", h.setFx)
	protected.PATCH("/settings", h.update)
}

type settingsResponse struct {
	MainCurrency      string      `json:"main_currency"`
	SecondaryCurrency string      `json:"secondary_currency"`
	DisplayCurrency   string      `json:"display_currency"`
	Timezone          string      `json:"timezone"`
	FxMode            string      `json:"fx_mode"`
	FxConfigured      bool        `json:"fx_configured"`
	Rate              *money.Rate `json:"rate"`
	RateUpdatedAt     *time.Time  `json:"rate_updated_at"`
}

type setFxRequest struct {
	MainCurrency      string  `json:"main_currency"`
	SecondaryCurrency string  `json:"secondary_currency"`
	Rate              *string `json:"rate"`
}

type updateRequest struct {
	DisplayCurrency *string `json:"display_currency"`
	Timezone        *string `json:"timezone"`
}

func (h *Handler) currencies(c *gin.Context) {
	// El catálogo casi no cambia: los clientes pueden guardarlo un día.
	c.Header("Cache-Control", "public, max-age=86400")
	apierror.OK(c, currency.Supported)
}

func (h *Handler) get(c *gin.Context) {
	settings, err := h.service.Get(c.Request.Context(), userID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toResponse(settings))
}

func (h *Handler) setFx(c *gin.Context) {
	var req setFxRequest
	if !bind(c, &req) {
		return
	}
	settings, err := h.service.SetFx(c.Request.Context(), userID(c), app.SetFxInput{
		Main: req.MainCurrency, Secondary: req.SecondaryCurrency, Rate: req.Rate,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toResponse(settings))
}

func (h *Handler) update(c *gin.Context) {
	var req updateRequest
	if !bind(c, &req) {
		return
	}
	settings, err := h.service.Update(c.Request.Context(), userID(c), app.UpdateInput{
		DisplayCurrency: req.DisplayCurrency, Timezone: req.Timezone,
	})
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toResponse(settings))
}

func userID(c *gin.Context) uuid.UUID {
	principal, _ := auth.FromContext(c.Request.Context())
	return principal.UserID
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

func respondError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		fields := make([]apierror.FieldError, 0, len(validation.Fields))
		for _, f := range validation.Fields {
			fields = append(fields, apierror.FieldError{Field: f.Field, Code: f.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.Is(err, domain.ErrFxNotConfigured):
		apierror.Respond(c, errFxNotConfigured)
	default:
		// Incluye un perfil que falta: todo usuario lo tiene desde el alta, así
		// que si no está es un error nuestro (500), no del cliente.
		apierror.Respond(c, err)
	}
}

func toResponse(s app.Settings) settingsResponse {
	out := settingsResponse{
		MainCurrency:      s.Main,
		SecondaryCurrency: s.Secondary,
		DisplayCurrency:   s.DisplayCurrency,
		Timezone:          s.Timezone,
		FxMode:            s.FxMode,
		FxConfigured:      s.IsConfigured(),
	}
	if s.Rate != nil {
		rate := money.NewRate(*s.Rate)
		out.Rate = &rate
		updated := s.RateUpdatedAt.UTC()
		out.RateUpdatedAt = &updated
	}
	return out
}
