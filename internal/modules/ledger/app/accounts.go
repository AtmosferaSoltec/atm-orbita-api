package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
)

// NewAccount es una cuenta por guardar.
type NewAccount struct {
	ID               uuid.UUID
	Name             string
	Type             domain.AccountType
	Currency         string
	InitialBalance   decimal.Decimal
	IncludeInSavings bool
}

// StoredAccount es una cuenta con su dueño, su saldo inicial y si está archivada.
type StoredAccount struct {
	domain.Account
	UserID         uuid.UUID
	InitialBalance decimal.Decimal
	Archived       bool
}

// sameContent dice si un POST con un id ya usado es un reintento de la app.
func (s StoredAccount) sameContent(n NewAccount) bool {
	return s.Name == n.Name && s.Type == n.Type && s.Currency == n.Currency &&
		s.InitialBalance.Equal(n.InitialBalance) && s.IncludeInSavings == n.IncludeInSavings
}

// Accounts guarda cuentas. Todo método recibe el usuario.
type Accounts interface {
	// List devuelve las no archivadas con su saldo.
	List(ctx context.Context, userID uuid.UUID) ([]domain.Account, error)
	// Insert devuelve inserted=false si el id ya existía (de este u otro usuario).
	Insert(ctx context.Context, userID uuid.UUID, a NewAccount, at time.Time) (inserted bool, err error)
	// Get devuelve domain.ErrNotFound si no existe o es de otro usuario.
	Get(ctx context.Context, userID, id uuid.UUID) (StoredAccount, error)
	// Update y Archive devuelven domain.ErrNotFound si no hay una activa propia con ese id.
	Update(ctx context.Context, userID uuid.UUID, a domain.Account) error
	Archive(ctx context.Context, userID, id uuid.UUID) error
}

// FxSource da las monedas y el tipo de cambio del usuario (módulo profile).
type FxSource interface {
	Fx(ctx context.Context, userID uuid.UUID) (domain.Fx, error)
}

// AccountService son los casos de uso de cuentas.
type AccountService struct {
	accounts Accounts
	fx       FxSource
	clock    clock.Clock
}

// NewAccountService crea el servicio.
func NewAccountService(accounts Accounts, fx FxSource, c clock.Clock) *AccountService {
	return &AccountService{accounts: accounts, fx: fx, clock: c}
}

// AccountList son las cuentas activas y el ahorro total.
type AccountList struct {
	Accounts []domain.Account
	Savings  domain.Savings
}

// List devuelve las cuentas activas, en su orden, con el ahorro total.
func (s *AccountService) List(ctx context.Context, userID uuid.UUID) (AccountList, error) {
	accounts, err := s.accounts.List(ctx, userID)
	if err != nil {
		return AccountList{}, err
	}
	fx, err := s.fx.Fx(ctx, userID)
	if err != nil {
		return AccountList{}, err
	}
	return AccountList{Accounts: accounts, Savings: domain.ComputeSavings(accounts, fx)}, nil
}

// CreateAccountInput es el formulario Nueva cuenta. ID lo puede mandar la app.
type CreateAccountInput struct {
	ID               *uuid.UUID
	Name             string
	Type             domain.AccountType
	Currency         string
	InitialBalance   *string
	IncludeInSavings *bool
}

// Create guarda una cuenta nueva. Un reintento con el mismo id y el mismo
// contenido devuelve la existente (created=false).
func (s *AccountService) Create(ctx context.Context, userID uuid.UUID, in CreateAccountInput) (domain.Account, bool, error) {
	var problems domain.ValidationError
	account := NewAccount{ID: ids.New(), IncludeInSavings: true}
	account.Name = domain.ValidateName(in.Name, &problems)
	domain.ValidateAccountType(in.Type, &problems)
	domain.ValidateCurrency(in.Currency, &problems)
	account.InitialBalance = domain.ValidateInitialBalance(in.InitialBalance, &problems)
	if err := problems.OrNil(); err != nil {
		return domain.Account{}, false, err
	}
	account.Type, account.Currency = in.Type, in.Currency
	if in.ID != nil {
		account.ID = *in.ID
	}
	if in.IncludeInSavings != nil {
		account.IncludeInSavings = *in.IncludeInSavings
	}

	fx, err := s.fx.Fx(ctx, userID)
	if err != nil {
		return domain.Account{}, false, err
	}
	if err := fx.RequireUsable(account.Currency); err != nil {
		return domain.Account{}, false, err
	}

	inserted, err := s.accounts.Insert(ctx, userID, account, s.clock.Now())
	if err != nil {
		return domain.Account{}, false, err
	}
	if inserted {
		return domain.Account{
			ID: account.ID, Name: account.Name, Type: account.Type, Currency: account.Currency,
			Balance: account.InitialBalance, IncludeInSavings: account.IncludeInSavings,
		}, true, nil
	}
	existing, err := s.accounts.Get(ctx, userID, account.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Account{}, false, domain.ErrIDTaken
	}
	if err != nil {
		return domain.Account{}, false, err
	}
	if existing.Archived || !existing.sameContent(account) {
		return domain.Account{}, false, domain.ErrIDTaken
	}
	return existing.Account, false, nil
}

// UpdateAccountInput es Editar cuenta: nombre, tipo y si cuenta en el ahorro.
// La moneda y el saldo inicial no cambian.
type UpdateAccountInput struct {
	Name             *string
	Type             *domain.AccountType
	IncludeInSavings *bool
}

// Update cambia nombre, tipo o si cuenta en el ahorro.
func (s *AccountService) Update(ctx context.Context, userID, id uuid.UUID, in UpdateAccountInput) (domain.Account, error) {
	current, err := s.accounts.Get(ctx, userID, id)
	if err != nil {
		return domain.Account{}, err
	}
	if current.Archived {
		return domain.Account{}, domain.ErrNotFound
	}
	next := current.Account
	var problems domain.ValidationError
	if in.Name != nil {
		next.Name = domain.ValidateName(*in.Name, &problems)
	}
	if in.Type != nil {
		domain.ValidateAccountType(*in.Type, &problems)
		next.Type = *in.Type
	}
	if err := problems.OrNil(); err != nil {
		return domain.Account{}, err
	}
	if in.IncludeInSavings != nil {
		next.IncludeInSavings = *in.IncludeInSavings
	}
	if err := s.accounts.Update(ctx, userID, next); err != nil {
		return domain.Account{}, err
	}
	return next, nil
}

// Archive deja de mostrar la cuenta; sus movimientos se conservan. Archivar una
// ya archivada no es un error.
func (s *AccountService) Archive(ctx context.Context, userID, id uuid.UUID) error {
	current, err := s.accounts.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if current.Archived {
		return nil
	}
	return s.accounts.Archive(ctx, userID, id)
}

// CreateInitial crea "Efectivo" en la moneda principal (gancho de alta).
func (s *AccountService) CreateInitial(ctx context.Context, userID uuid.UUID, mainCurrency string) error {
	_, err := s.accounts.Insert(ctx, userID, NewAccount{
		ID: ids.New(), Name: domain.InitialAccountName, Type: domain.AccountCash,
		Currency: mainCurrency, InitialBalance: decimal.Zero, IncludeInSavings: true,
	}, s.clock.Now())
	return err
}
