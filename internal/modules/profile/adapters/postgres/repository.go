// Package postgres implementa los puertos de guardado del perfil con las
// consultas de queries.sql (generadas por sqlc en sqlcgen/).
package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// DB da la conexión de la transacción en curso, o el pool (tx.Runner).
type DB interface {
	DB(ctx context.Context) tx.DBTX
}

// Profiles implementa app.Profiles.
type Profiles struct {
	db DB
}

// NewProfiles crea el repositorio.
func NewProfiles(db DB) Profiles { return Profiles{db: db} }

func (r Profiles) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// Create guarda el perfil de un usuario nuevo.
func (r Profiles) Create(ctx context.Context, userID uuid.UUID, p app.StoredProfile) error {
	return r.q(ctx).CreateProfile(ctx, sqlcgen.CreateProfileParams{
		UserID: userID, MainCurrency: p.Main, SecondaryCurrency: p.Secondary,
		DisplayCurrency: p.Display, Timezone: p.Timezone,
	})
}

// Get lee el perfil.
func (r Profiles) Get(ctx context.Context, userID uuid.UUID) (app.StoredProfile, error) {
	row, err := r.q(ctx).GetProfile(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.StoredProfile{}, domain.ErrNotFound
	}
	if err != nil {
		return app.StoredProfile{}, err
	}
	return app.StoredProfile{
		Main: row.MainCurrency, Secondary: row.SecondaryCurrency, Display: row.DisplayCurrency,
		Timezone: row.Timezone, FxMode: row.FxMode,
	}, nil
}

// Lock lee el perfil y lo bloquea hasta el fin de la transacción.
func (r Profiles) Lock(ctx context.Context, userID uuid.UUID) (app.StoredProfile, error) {
	row, err := r.q(ctx).LockProfile(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.StoredProfile{}, domain.ErrNotFound
	}
	if err != nil {
		return app.StoredProfile{}, err
	}
	return app.StoredProfile{
		Main: row.MainCurrency, Secondary: row.SecondaryCurrency, Display: row.DisplayCurrency,
		Timezone: row.Timezone, FxMode: row.FxMode,
	}, nil
}

// Update guarda las monedas, la visualización y la zona.
func (r Profiles) Update(ctx context.Context, userID uuid.UUID, p app.StoredProfile) error {
	return r.q(ctx).UpdateProfile(ctx, sqlcgen.UpdateProfileParams{
		UserID: userID, MainCurrency: p.Main, SecondaryCurrency: p.Secondary,
		DisplayCurrency: p.Display, Timezone: p.Timezone,
	})
}

// InsertRate agrega una fila al historial de tipos de cambio.
func (r Profiles) InsertRate(ctx context.Context, rate app.NewRate) error {
	userID := rate.UserID
	return r.q(ctx).InsertExchangeRate(ctx, sqlcgen.InsertExchangeRateParams{
		ID: rate.ID, UserID: &userID, FromCurrency: rate.From, ToCurrency: rate.To,
		Rate: rate.Rate, Source: rate.Source,
	})
}

// LatestRate busca el tipo de cambio más reciente del par, en cualquier sentido.
func (r Profiles) LatestRate(ctx context.Context, userID uuid.UUID, a, b string) (app.StoredRate, bool, error) {
	row, err := r.q(ctx).LatestUserRate(ctx, sqlcgen.LatestUserRateParams{
		UserID: &userID, FromCurrency: a, ToCurrency: b,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return app.StoredRate{}, false, nil
	}
	if err != nil {
		return app.StoredRate{}, false, err
	}
	return app.StoredRate{From: row.FromCurrency, To: row.ToCurrency, Rate: row.Rate, CreatedAt: row.CreatedAt}, true, nil
}
