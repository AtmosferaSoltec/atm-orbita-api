package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
)

// fakeCategories imita al repositorio, incluido el índice único por tipo y nombre.
type fakeCategories struct {
	rows []StoredCategory
}

func (f *fakeCategories) find(id uuid.UUID) int {
	for i, r := range f.rows {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func (f *fakeCategories) nameTaken(userID uuid.UUID, c domain.Category) bool {
	for _, r := range f.rows {
		if r.UserID == userID && !r.Archived && r.ID != c.ID && r.Kind == c.Kind && r.NameKey() == c.NameKey() {
			return true
		}
	}
	return false
}

func (f *fakeCategories) List(_ context.Context, userID uuid.UUID) ([]domain.Category, error) {
	var out []domain.Category
	for _, r := range f.rows {
		if r.UserID == userID && !r.Archived {
			out = append(out, r.Category)
		}
	}
	return out, nil
}

func (f *fakeCategories) Insert(_ context.Context, userID uuid.UUID, c domain.Category, _ time.Time) (bool, error) {
	if f.find(c.ID) >= 0 {
		return false, nil
	}
	if f.nameTaken(userID, c) {
		return false, domain.ErrNameTaken
	}
	f.rows = append(f.rows, StoredCategory{Category: c, UserID: userID})
	return true, nil
}

func (f *fakeCategories) Get(_ context.Context, userID, id uuid.UUID) (StoredCategory, error) {
	i := f.find(id)
	if i < 0 || f.rows[i].UserID != userID {
		return StoredCategory{}, domain.ErrNotFound
	}
	return f.rows[i], nil
}

func (f *fakeCategories) Update(_ context.Context, userID uuid.UUID, c domain.Category) error {
	i := f.find(c.ID)
	if i < 0 || f.rows[i].UserID != userID || f.rows[i].Archived {
		return domain.ErrNotFound
	}
	if f.nameTaken(userID, c) {
		return domain.ErrNameTaken
	}
	f.rows[i].Category = c
	return nil
}

func (f *fakeCategories) Archive(_ context.Context, userID, id uuid.UUID) error {
	i := f.find(id)
	if i < 0 || f.rows[i].UserID != userID || f.rows[i].Archived {
		return domain.ErrNotFound
	}
	f.rows[i].Archived = true
	return nil
}

func newService() (*CategoryService, *fakeCategories, uuid.UUID) {
	fake := &fakeCategories{}
	return NewCategoryService(fake, clock.Fixed{At: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}), fake, uuid.New()
}

func TestCreateInitialGivesTheNineInOrder(t *testing.T) {
	s, _, user := newService()
	require.NoError(t, s.CreateInitial(context.Background(), user))
	list, err := s.List(context.Background(), user)
	require.NoError(t, err)
	require.Len(t, list, 9)
	assert.Equal(t, "Alimentación", list[0].Name)
	assert.Equal(t, "Otros ingresos", list[8].Name)
}

func TestCreateValidatesInTheAppOrder(t *testing.T) {
	s, _, user := newService()
	_, _, err := s.Create(context.Background(), user, CreateInput{Name: " ", Kind: "x", Color: "red"})
	var problems *domain.ValidationError
	require.ErrorAs(t, err, &problems)
	assert.Equal(t, []domain.FieldError{
		{Field: "name", Code: domain.CodeNameRequired},
		{Field: "kind", Code: domain.CodeKindInvalid},
		{Field: "color", Code: domain.CodeColorInvalid},
	}, problems.Fields)
}

func TestCreateWithTheSameIDIsARetry(t *testing.T) {
	s, _, user := newService()
	id := uuid.New()
	in := CreateInput{ID: &id, Name: "Mascotas", Kind: domain.KindExpense, Color: "#0E7490"}

	first, created, err := s.Create(context.Background(), user, in)
	require.NoError(t, err)
	assert.True(t, created)

	again, created, err := s.Create(context.Background(), user, in)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first, again)

	in.Color = "#64748B"
	_, _, err = s.Create(context.Background(), user, in)
	assert.ErrorIs(t, err, domain.ErrIDTaken)

	// Otro usuario con el mismo id no ve la categoría: también es un choque.
	in.Color = "#0E7490"
	_, _, err = s.Create(context.Background(), uuid.New(), in)
	assert.ErrorIs(t, err, domain.ErrIDTaken)
}

func TestUpdateKeepsTheKindAndCanChangeCase(t *testing.T) {
	s, _, user := newService()
	ocio, _, err := s.Create(context.Background(), user, CreateInput{Name: "Ocio", Kind: domain.KindExpense, Color: "#BE185D"})
	require.NoError(t, err)

	name := "  OCIO "
	updated, err := s.Update(context.Background(), user, ocio.ID, UpdateInput{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, "OCIO", updated.Name)
	assert.Equal(t, domain.KindExpense, updated.Kind)
	assert.Equal(t, "#BE185D", updated.Color)

	bad := "rojo"
	_, err = s.Update(context.Background(), user, ocio.ID, UpdateInput{Color: &bad})
	var problems *domain.ValidationError
	assert.ErrorAs(t, err, &problems)
}

func TestArchiveIsIdempotentAndHidesIt(t *testing.T) {
	s, _, user := newService()
	c, _, err := s.Create(context.Background(), user, CreateInput{Name: "Ocio", Kind: domain.KindExpense, Color: "#BE185D"})
	require.NoError(t, err)

	require.NoError(t, s.Archive(context.Background(), user, c.ID))
	require.NoError(t, s.Archive(context.Background(), user, c.ID))
	list, _ := s.List(context.Background(), user)
	assert.Empty(t, list)

	name := "Otro"
	_, err = s.Update(context.Background(), user, c.ID, UpdateInput{Name: &name})
	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.ErrorIs(t, s.Archive(context.Background(), uuid.New(), c.ID), domain.ErrNotFound)
}
