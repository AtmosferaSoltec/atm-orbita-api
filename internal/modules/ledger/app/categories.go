// Package app son los casos de uso del libro del usuario y los puertos que necesitan.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
)

// StoredCategory es una categoría con su dueño y si está archivada.
type StoredCategory struct {
	domain.Category
	UserID   uuid.UUID
	Archived bool
}

// Categories guarda categorías. Todo método recibe el usuario: nadie busca por
// id solamente.
type Categories interface {
	List(ctx context.Context, userID uuid.UUID) ([]domain.Category, error)
	// Insert devuelve inserted=false si el id ya existía (de este u otro usuario), y
	// domain.ErrNameTaken si el nombre ya existe en ese tipo.
	Insert(ctx context.Context, userID uuid.UUID, c domain.Category, at time.Time) (inserted bool, err error)
	// Get devuelve domain.ErrNotFound si no existe o es de otro usuario.
	Get(ctx context.Context, userID, id uuid.UUID) (StoredCategory, error)
	// Update y Archive devuelven domain.ErrNotFound si no hay una activa propia con ese id.
	Update(ctx context.Context, userID uuid.UUID, c domain.Category) error
	Archive(ctx context.Context, userID, id uuid.UUID) error
}

// CategoryService son los casos de uso de categorías.
type CategoryService struct {
	categories Categories
	clock      clock.Clock
}

// NewCategoryService crea el servicio.
func NewCategoryService(categories Categories, c clock.Clock) *CategoryService {
	return &CategoryService{categories: categories, clock: c}
}

// List devuelve las categorías no archivadas, en el orden en que se crearon.
func (s *CategoryService) List(ctx context.Context, userID uuid.UUID) ([]domain.Category, error) {
	return s.categories.List(ctx, userID)
}

// CreateInput es el formulario Nueva categoría. ID lo puede mandar la app.
type CreateInput struct {
	ID    *uuid.UUID
	Name  string
	Kind  domain.Kind
	Color string
}

// Create guarda una categoría nueva. Si la app reintenta con el mismo id y el
// mismo contenido, devuelve la existente (created=false) en lugar de duplicarla.
func (s *CategoryService) Create(ctx context.Context, userID uuid.UUID, in CreateInput) (domain.Category, bool, error) {
	var problems domain.ValidationError
	name := domain.ValidateName(in.Name, &problems)
	domain.ValidateKind(in.Kind, &problems)
	domain.ValidateColor(in.Color, &problems)
	if err := problems.OrNil(); err != nil {
		return domain.Category{}, false, err
	}

	category := domain.Category{ID: ids.New(), Name: name, Kind: in.Kind, Color: in.Color}
	if in.ID != nil {
		category.ID = *in.ID
	}
	inserted, err := s.categories.Insert(ctx, userID, category, s.clock.Now())
	if err != nil || inserted {
		return category, inserted, err
	}
	existing, err := s.categories.Get(ctx, userID, category.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Category{}, false, domain.ErrIDTaken
	}
	if err != nil {
		return domain.Category{}, false, err
	}
	if existing.Archived || !existing.SameContent(category) {
		return domain.Category{}, false, domain.ErrIDTaken
	}
	return existing.Category, false, nil
}

// UpdateInput es Editar categoría: nombre y color. El tipo no cambia.
type UpdateInput struct {
	Name  *string
	Color *string
}

// Update renombra o cambia el color. Renombrar "ocio" a "Ocio" no choca consigo misma.
func (s *CategoryService) Update(ctx context.Context, userID, id uuid.UUID, in UpdateInput) (domain.Category, error) {
	current, err := s.categories.Get(ctx, userID, id)
	if err != nil {
		return domain.Category{}, err
	}
	if current.Archived {
		return domain.Category{}, domain.ErrNotFound
	}
	next := current.Category
	var problems domain.ValidationError
	if in.Name != nil {
		next.Name = domain.ValidateName(*in.Name, &problems)
	}
	if in.Color != nil {
		domain.ValidateColor(*in.Color, &problems)
		next.Color = *in.Color
	}
	if err := problems.OrNil(); err != nil {
		return domain.Category{}, err
	}
	if err := s.categories.Update(ctx, userID, next); err != nil {
		return domain.Category{}, err
	}
	return next, nil
}

// Archive deja de mostrar la categoría y libera su nombre; los movimientos que ya
// la usan la conservan. Archivar una ya archivada no es un error.
func (s *CategoryService) Archive(ctx context.Context, userID, id uuid.UUID) error {
	current, err := s.categories.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if current.Archived {
		return nil
	}
	return s.categories.Archive(ctx, userID, id)
}

// CreateInitial crea las 9 categorías con que nace un usuario (gancho de alta).
func (s *CategoryService) CreateInitial(ctx context.Context, userID uuid.UUID) error {
	at := s.clock.Now()
	for _, initial := range domain.InitialCategories {
		category := domain.Category{ID: ids.New(), Name: initial.Name, Kind: initial.Kind, Color: initial.Color}
		if _, err := s.categories.Insert(ctx, userID, category, at); err != nil {
			return err
		}
	}
	return nil
}
