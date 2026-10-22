package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// StoredTransfer es una transferencia y si está eliminada.
type StoredTransfer struct {
	domain.Transfer
	Deleted bool
}

// Transfers guarda transferencias. Todo método recibe el usuario.
type Transfers interface {
	// Insert devuelve inserted=false si el id ya existía (de este u otro usuario).
	Insert(ctx context.Context, userID uuid.UUID, t domain.Transfer, at time.Time) (inserted bool, err error)
	// Get devuelve domain.ErrNotFound si no existe o es de otro usuario.
	Get(ctx context.Context, userID, id uuid.UUID) (StoredTransfer, error)
	// Update y Delete devuelven domain.ErrNotFound si no hay una propia sin eliminar.
	Update(ctx context.Context, userID uuid.UUID, t domain.Transfer) error
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

// TransferService son los casos de uso de transferencias.
type TransferService struct {
	transfers Transfers
	accounts  Accounts
	profile   Profile
	clock     clock.Clock
}

// NewTransferService crea el servicio.
func NewTransferService(transfers Transfers, accounts Accounts, profile Profile, c clock.Clock) *TransferService {
	return &TransferService{transfers: transfers, accounts: accounts, profile: profile, clock: c}
}

// OptionalText distingue un campo que no vino (Set=false) de uno que vino
// vacío (Set=true, Value=nil).
type OptionalText struct {
	Set   bool
	Value *string
}

// TransferInput es Transferir / Editar transferencia. Al editar, lo que no viene
// se queda como estaba.
type TransferInput struct {
	FromAccountID *uuid.UUID
	ToAccountID   *uuid.UUID
	FromAmount    *string
	ToAmount      *string
	ExchangeRate  OptionalText
	Note          *string
	OccurredOn    *string
}

// Create guarda una transferencia. Un reintento con el mismo id y el mismo
// contenido devuelve la existente (created=false).
func (s *TransferService) Create(ctx context.Context, userID uuid.UUID, id *uuid.UUID, in TransferInput) (domain.TransferEntry, bool, error) {
	entry, err := s.build(ctx, userID, in, nil)
	if err != nil {
		return domain.TransferEntry{}, false, err
	}
	entry.ID = ids.New()
	if id != nil {
		entry.ID = *id
	}
	inserted, err := s.transfers.Insert(ctx, userID, entry.Transfer, s.clock.Now())
	if err != nil || inserted {
		return entry, inserted, err
	}
	existing, err := s.transfers.Get(ctx, userID, entry.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.TransferEntry{}, false, domain.ErrIDTaken
	}
	if err != nil {
		return domain.TransferEntry{}, false, err
	}
	if existing.Deleted || !domain.SameTransfer(existing.Transfer, entry.Transfer, in.OccurredOn != nil) {
		return domain.TransferEntry{}, false, domain.ErrIDTaken
	}
	entry.Transfer = existing.Transfer
	return entry, false, nil
}

// Update cambia una transferencia; vuelve a revisar todo con los datos nuevos.
func (s *TransferService) Update(ctx context.Context, userID, id uuid.UUID, in TransferInput) (domain.TransferEntry, error) {
	current, err := s.transfers.Get(ctx, userID, id)
	if err != nil {
		return domain.TransferEntry{}, err
	}
	if current.Deleted {
		return domain.TransferEntry{}, domain.ErrNotFound
	}
	merged := mergeTransfer(current.Transfer, in)
	entry, err := s.build(ctx, userID, merged, &current.Transfer)
	if err != nil {
		return domain.TransferEntry{}, err
	}
	entry.ID = id
	if err := s.transfers.Update(ctx, userID, entry.Transfer); err != nil {
		return domain.TransferEntry{}, err
	}
	return entry, nil
}

// Delete elimina (de forma lógica) la transferencia; los dos saldos vuelven a
// como estaban. Eliminar una ya eliminada no es un error.
func (s *TransferService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	current, err := s.transfers.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if current.Deleted {
		return nil
	}
	return s.transfers.Delete(ctx, userID, id)
}

// mergeTransfer completa lo que no vino con lo que ya estaba guardado.
func mergeTransfer(current domain.Transfer, in TransferInput) TransferInput {
	out := in
	if out.FromAccountID == nil {
		out.FromAccountID = &current.FromAccountID
	}
	if out.ToAccountID == nil {
		out.ToAccountID = &current.ToAccountID
	}
	if out.FromAmount == nil {
		text := current.FromAmount.StringFixed(money.AmountScale)
		out.FromAmount = &text
	}
	if out.ToAmount == nil {
		text := current.ToAmount.StringFixed(money.AmountScale)
		out.ToAmount = &text
	}
	if !out.ExchangeRate.Set && current.ExchangeRate != nil {
		text := current.ExchangeRate.StringFixed(money.RateScale)
		out.ExchangeRate = OptionalText{Set: true, Value: &text}
	}
	if out.Note == nil {
		out.Note = &current.Note
	}
	if out.OccurredOn == nil {
		text := current.OccurredOn.String()
		out.OccurredOn = &text
	}
	return out
}

// build revisa una transferencia completa en el orden de Android (cuentas
// distintas, montos, tipo de cambio, nota, fecha). current es la guardada al
// editar: una cuenta archivada que ya era suya se puede quedar.
func (s *TransferService) build(ctx context.Context, userID uuid.UUID, in TransferInput, current *domain.Transfer) (domain.TransferEntry, error) {
	today, err := s.profile.Today(ctx, userID)
	if err != nil {
		return domain.TransferEntry{}, err
	}
	var problems domain.ValidationError
	var t domain.Transfer
	if in.FromAccountID == nil {
		problems.Add("from_account_id", domain.CodeAccountRequired)
	}
	if in.ToAccountID == nil {
		problems.Add("to_account_id", domain.CodeAccountRequired)
	}
	distinct := in.FromAccountID != nil && in.ToAccountID != nil && *in.FromAccountID != *in.ToAccountID
	if in.FromAccountID != nil && in.ToAccountID != nil && !distinct {
		problems.Add("to_account_id", domain.CodeSameAccount)
	}
	from, fromOK := domain.ValidateAmountField("from_amount", in.FromAmount, &problems)
	to, toOK := domain.ValidateAmountField("to_amount", in.ToAmount, &problems)
	t.FromAmount, t.ToAmount = from, to

	var fromAccount, toAccount StoredAccount
	if distinct {
		t.FromAccountID, t.ToAccountID = *in.FromAccountID, *in.ToAccountID
		keepFrom := current != nil && current.FromAccountID == t.FromAccountID
		keepTo := current != nil && current.ToAccountID == t.ToAccountID
		if fromAccount, err = s.account(ctx, userID, t.FromAccountID, keepFrom); err != nil {
			return domain.TransferEntry{}, err
		}
		if toAccount, err = s.account(ctx, userID, t.ToAccountID, keepTo); err != nil {
			return domain.TransferEntry{}, err
		}
		t.ExchangeRate = domain.CheckTransferRate(fromAccount.Currency, toAccount.Currency, from, to,
			fromOK && toOK, in.ExchangeRate.Value, &problems)
	}
	note := ""
	if in.Note != nil {
		note = *in.Note
	}
	t.Note = domain.ValidateTextField("note", note, &problems)
	t.OccurredOn = domain.ValidateDate(in.OccurredOn, today, &problems)
	if err := problems.OrNil(); err != nil {
		return domain.TransferEntry{}, err
	}
	return domain.TransferEntry{Transfer: t, From: entryAccount(fromAccount), To: entryAccount(toAccount)}, nil
}

// account busca una cuenta del usuario; una archivada solo vale si ya era la suya.
func (s *TransferService) account(ctx context.Context, userID, id uuid.UUID, keep bool) (StoredAccount, error) {
	if keep {
		return s.accounts.Get(ctx, userID, id)
	}
	return usableAccount(ctx, s.accounts, userID, id)
}

func entryAccount(a StoredAccount) domain.EntryAccount {
	return domain.EntryAccount{ID: a.ID, Name: a.Name, Type: a.Type, Currency: a.Currency}
}
