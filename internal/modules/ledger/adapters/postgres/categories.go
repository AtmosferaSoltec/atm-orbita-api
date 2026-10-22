// Package postgres implementa los puertos de guardado del libro con las
// consultas de *.sql (generadas por sqlc en sqlcgen/).
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

const (
	uniqueViolation        = "23505"
	categoryNameConstraint = "categories_user_kind_name_key"
)

// DB da la conexión de la transacción en curso, o el pool (tx.Runner).
type DB interface {
	DB(ctx context.Context) tx.DBTX
}

// Categories implementa app.Categories.
type Categories struct {
	db DB
}

// NewCategories crea el repositorio.
func NewCategories(db DB) Categories { return Categories{db: db} }

func (r Categories) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// List devuelve las no archivadas del usuario.
func (r Categories) List(ctx context.Context, userID uuid.UUID) ([]domain.Category, error) {
	rows, err := r.q(ctx).ListCategories(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Category{ID: row.ID, Name: row.Name, Kind: domain.Kind(row.Kind), Color: row.Color})
	}
	return out, nil
}

// Insert guarda la categoría; el índice único decide si el nombre ya existe.
func (r Categories) Insert(ctx context.Context, userID uuid.UUID, c domain.Category, at time.Time) (bool, error) {
	key := c.NameKey()
	n, err := r.q(ctx).InsertCategory(ctx, sqlcgen.InsertCategoryParams{
		ID: c.ID, UserID: userID, Name: c.Name, NameNormalized: &key, Kind: string(c.Kind), Color: c.Color, CreatedAt: at,
	})
	if isNameTaken(err) {
		return false, domain.ErrNameTaken
	}
	return n == 1, err
}

// Get busca una categoría del usuario.
func (r Categories) Get(ctx context.Context, userID, id uuid.UUID) (app.StoredCategory, error) {
	row, err := r.q(ctx).GetCategory(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return app.StoredCategory{}, domain.ErrNotFound
	}
	if err != nil {
		return app.StoredCategory{}, err
	}
	return app.StoredCategory{
		Category: domain.Category{ID: row.ID, Name: row.Name, Kind: domain.Kind(row.Kind), Color: row.Color},
		UserID:   row.UserID,
		Archived: row.ArchivedAt != nil,
	}, nil
}

// Update cambia nombre y color de una categoría activa propia.
func (r Categories) Update(ctx context.Context, userID uuid.UUID, c domain.Category) error {
	key := c.NameKey()
	n, err := r.q(ctx).UpdateCategory(ctx, sqlcgen.UpdateCategoryParams{
		ID: c.ID, UserID: userID, Name: c.Name, NameNormalized: &key, Color: c.Color,
	})
	if isNameTaken(err) {
		return domain.ErrNameTaken
	}
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// Archive archiva una categoría activa propia y libera su nombre.
func (r Categories) Archive(ctx context.Context, userID, id uuid.UUID) error {
	n, err := r.q(ctx).ArchiveCategory(ctx, sqlcgen.ArchiveCategoryParams{ID: id, UserID: userID})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

func isNameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == categoryNameConstraint
}

// Find devuelve las categorías pedidas del usuario, también las archivadas.
func (r Categories) Find(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]app.StoredCategory, error) {
	rows, err := r.q(ctx).ListCategoriesByIDs(ctx, sqlcgen.ListCategoriesByIDsParams{UserID: userID, Ids: ids})
	if err != nil {
		return nil, err
	}
	out := make([]app.StoredCategory, 0, len(rows))
	for _, row := range rows {
		out = append(out, app.StoredCategory{
			Category: domain.Category{ID: row.ID, Name: row.Name, Kind: domain.Kind(row.Kind), Color: row.Color},
			UserID:   userID,
			Archived: row.ArchivedAt != nil,
		})
	}
	return out, nil
}
