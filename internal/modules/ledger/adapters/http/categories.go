// Package http traduce HTTP ⇄ casos de uso del libro.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
)

// CategoryService son los casos de uso de categorías que usa el handler.
type CategoryService interface {
	List(ctx context.Context, userID uuid.UUID) ([]domain.Category, error)
	Create(ctx context.Context, userID uuid.UUID, in app.CreateInput) (domain.Category, bool, error)
	Update(ctx context.Context, userID, id uuid.UUID, in app.UpdateInput) (domain.Category, error)
	Archive(ctx context.Context, userID, id uuid.UUID) error
}

const (
	codeCategoryNameTaken = "CATEGORY_NAME_TAKEN"
	codeNameTaken         = "NAME_TAKEN"
)

var errCategoryNameTaken = &apierror.Error{
	Status:  http.StatusConflict,
	Code:    codeCategoryNameTaken,
	Message: "Ya tienes una categoría de este tipo con ese nombre.",
	Details: []apierror.FieldError{{Field: "name", Code: codeNameTaken}},
}

// El mismo code y mensaje que da el perfil.
var errFxNotConfigured = apierror.New(http.StatusUnprocessableEntity, "FX_RATE_NOT_CONFIGURED",
	"Configura tu tipo de cambio en Ajustes para usar otra moneda.")

// Categories atiende la pantalla Categorías.
type Categories struct {
	service CategoryService
}

// NewCategories crea el handler.
func NewCategories(service CategoryService) *Categories { return &Categories{service: service} }

// Routes monta las rutas, todas privadas.
func (h *Categories) Routes(protected gin.IRoutes) {
	protected.GET("/categories", h.list)
	protected.POST("/categories", h.create)
	protected.PATCH("/categories/:id", h.update)
	protected.POST("/categories/:id/archive", h.archive)
}

type categoryResponse struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Kind  string    `json:"kind"`
	Color string    `json:"color"`
}

type createRequest struct {
	ID    *string `json:"id"`
	Name  string  `json:"name"`
	Kind  string  `json:"kind"`
	Color string  `json:"color"`
}

type updateRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

func (h *Categories) list(c *gin.Context) {
	categories, err := h.service.List(c.Request.Context(), userID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	out := make([]categoryResponse, 0, len(categories))
	for _, category := range categories {
		out = append(out, toResponse(category))
	}
	apierror.OK(c, out)
}

func (h *Categories) create(c *gin.Context) {
	var req createRequest
	if !bind(c, &req) {
		return
	}
	in := app.CreateInput{Name: req.Name, Kind: domain.Kind(req.Kind), Color: req.Color}
	if req.ID != nil {
		id, err := uuid.Parse(*req.ID)
		if err != nil {
			apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "id", Code: domain.CodeInvalidFormat}))
			return
		}
		in.ID = &id
	}
	category, created, err := h.service.Create(c.Request.Context(), userID(c), in)
	if err != nil {
		respondError(c, err)
		return
	}
	if created {
		apierror.Created(c, toResponse(category))
		return
	}
	apierror.OK(c, toResponse(category))
}

func (h *Categories) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req updateRequest
	if !bind(c, &req) {
		return
	}
	category, err := h.service.Update(c.Request.Context(), userID(c), id, app.UpdateInput{Name: req.Name, Color: req.Color})
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toResponse(category))
}

func (h *Categories) archive(c *gin.Context) {
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

func toResponse(c domain.Category) categoryResponse {
	return categoryResponse{ID: c.ID, Name: c.Name, Kind: string(c.Kind), Color: c.Color}
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
	case errors.Is(err, domain.ErrNameTaken):
		apierror.Respond(c, errCategoryNameTaken)
	case errors.Is(err, domain.ErrIDTaken):
		apierror.Respond(c, apierror.ErrIDConflict)
	case errors.Is(err, domain.ErrFxNotConfigured):
		apierror.Respond(c, errFxNotConfigured)
	default:
		apierror.Respond(c, err)
	}
}
