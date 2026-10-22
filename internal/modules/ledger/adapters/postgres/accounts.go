package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
)

// Accounts implementa app.Accounts.
type Accounts struct {
	db DB
}

// NewAccounts crea el repositorio.
func NewAccounts(db DB) Accounts { return Accounts{db: db} }

func (r Accounts) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// List devuelve las activas del usuario con su saldo.
func (r Accounts) List(ctx context.Context, userID uuid.UUID) ([]domain.Account, error) {
	rows, err := r.q(ctx).ListAccounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Account, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Account{
			ID: row.ID, Name: row.Name, Type: domain.AccountType(row.Type), Currency: row.Currency,
			Balance: row.Balance, IncludeInSavings: row.IncludeInSavings,
		})
	}
	return out, nil
}

// Insert guarda la cuenta; un id repetido no se pisa.
func (r Accounts) Insert(ctx context.Context, userID uuid.UUID, a app.NewAccount, at time.Time) (bool, error) {
	n, err := r.q(ctx).InsertAccount(ctx, sqlcgen.InsertAccountParams{
		ID: a.ID, UserID: userID, Name: a.Name, Type: string(a.Type), Currency: a.Currency,
		InitialBalance: a.InitialBalance, IncludeInSavings: a.IncludeInSavings, CreatedAt: at,
	})
	return n == 1, err
}

// Get busca una cuenta del usuario.
func (r Accounts) Get(ctx context.Context, userID, id uuid.UUID) (app.StoredAccount, error) {
	row, err := r.q(ctx).GetAccount(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return app.StoredAccount{}, domain.ErrNotFound
	}
	if err != nil {
		return app.StoredAccount{}, err
	}
	return app.StoredAccount{
		Account: domain.Account{
			ID: row.ID, Name: row.Name, Type: domain.AccountType(row.Type), Currency: row.Currency,
			Balance: row.Balance, IncludeInSavings: row.IncludeInSavings,
		},
		UserID:         row.UserID,
		InitialBalance: row.InitialBalance,
		Archived:       row.ArchivedAt != nil,
	}, nil
}

// Update cambia nombre, tipo y si cuenta en el ahorro.
func (r Accounts) Update(ctx context.Context, userID uuid.UUID, a domain.Account) error {
	n, err := r.q(ctx).UpdateAccount(ctx, sqlcgen.UpdateAccountParams{
		ID: a.ID, UserID: userID, Name: a.Name, Type: string(a.Type), IncludeInSavings: a.IncludeInSavings,
	})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// Archive archiva una cuenta activa propia.
func (r Accounts) Archive(ctx context.Context, userID, id uuid.UUID) error {
	n, err := r.q(ctx).ArchiveAccount(ctx, sqlcgen.ArchiveAccountParams{ID: id, UserID: userID})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}
