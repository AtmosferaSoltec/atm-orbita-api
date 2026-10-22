package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Transactor corre varias escrituras juntas o ninguna (tx.Runner).
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// PaymentExpense es el egreso que crea el pago: la categoría y la descripción
// son las de la compra.
type PaymentExpense struct {
	PurchaseID  uuid.UUID
	AccountID   uuid.UUID
	CategoryID  uuid.UUID
	Description string
	Amount      decimal.Decimal
	OccurredOn  dates.LocalDate
}

// PaymentLedger crea el egreso de un pago en la transacción en curso (módulo
// ledger). Devuelve su id y el egreso con la forma de GET /entries. Errores:
// domain.ErrNotFound y domain.ErrAccountArchived.
type PaymentLedger interface {
	CreatePaymentExpense(ctx context.Context, userID uuid.UUID, in PaymentExpense) (uuid.UUID, any, error)
}

// PayInput es Marcar como pagada: con qué cuenta, cuánto se descontó de verdad
// (en la moneda de esa cuenta: el banco pudo aplicar su propio cambio) y cuándo.
type PayInput struct {
	AccountID  *uuid.UUID
	PaidAmount *string
	PaidOn     *string
}

// Payment es la compra ya pagada y el egreso que la pagó.
type Payment struct {
	Purchase    PurchaseView
	Transaction any
}

// Pay paga una compra pendiente: en una sola transacción crea el egreso en la
// cuenta que paga (con la categoría y la descripción de la compra) y la saca
// de pendientes. La compra se bloquea: de dos pagos simultáneos, el segundo la
// encuentra pagada.
func (s *Service) Pay(ctx context.Context, userID, purchaseID uuid.UUID, in PayInput) (Payment, error) {
	today, err := s.profile.Today(ctx, userID)
	if err != nil {
		return Payment{}, err
	}
	var problems domain.ValidationError
	if in.AccountID == nil {
		problems.Add("account_id", domain.CodeAccountRequired)
	}
	amount := decimal.Zero
	if parsed, err := parseAmount(in.PaidAmount); err != nil {
		problems.Add("paid_amount", domain.CodeAmountRequired)
	} else {
		amount = parsed
	}
	paidOn := today
	if in.PaidOn != nil {
		day, err := dates.Parse(*in.PaidOn)
		switch {
		case err != nil:
			problems.Add("paid_on", domain.CodeInvalidFormat)
		case day.After(today):
			problems.Add("paid_on", domain.CodeFutureDate)
		default:
			paidOn = day
		}
	}
	if err := problems.OrNil(); err != nil {
		return Payment{}, err
	}

	var out Payment
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		purchase, err := s.purchases.Lock(ctx, userID, purchaseID)
		if err != nil {
			return err
		}
		if purchase.Deleted {
			return domain.ErrNotFound
		}
		if purchase.Status == domain.StatusPaid {
			return domain.ErrPurchaseAlreadyPaid
		}
		transactionID, transaction, err := s.ledger.CreatePaymentExpense(ctx, userID, PaymentExpense{
			PurchaseID: purchase.ID, AccountID: *in.AccountID, CategoryID: purchase.CategoryID,
			Description: purchase.Description, Amount: amount, OccurredOn: paidOn,
		})
		if err != nil {
			return err
		}
		if err := s.purchases.MarkPaid(ctx, userID, purchase.ID, transactionID); err != nil {
			return err
		}
		card, err := s.cards.Get(ctx, userID, purchase.CardID)
		if err != nil {
			return err
		}
		found, err := s.categories.Find(ctx, userID, []uuid.UUID{purchase.CategoryID})
		if err != nil {
			return err
		}
		out = Payment{
			Purchase: PurchaseView{
				Purchase: purchase.Purchase, Status: domain.StatusPaid,
				Card: card.Card, Category: found[purchase.CategoryID].Category,
			},
			Transaction: transaction,
		}
		return nil
	})
	return out, err
}

// RevertPayment vuelve a pendiente la compra cuyo egreso de pago se eliminó, y
// desarchiva su tarjeta si hacía falta, para que ninguna deuda quede oculta.
// Corre en la transacción del libro. Si la compra ya no estaba pagada, no hace nada.
func (s *Service) RevertPayment(ctx context.Context, userID, purchaseID uuid.UUID) error {
	cardID, ok, err := s.purchases.Revert(ctx, userID, purchaseID)
	if err != nil || !ok {
		return err
	}
	return s.cards.Unarchive(ctx, userID, cardID)
}

// parseAmount lee un monto obligatorio y mayor que cero.
func parseAmount(text *string) (decimal.Decimal, error) {
	if text == nil {
		return decimal.Zero, money.ErrInvalidAmount
	}
	amount, err := money.ParseAmount(*text)
	if err != nil {
		return decimal.Zero, err
	}
	if !amount.IsPositive() {
		return decimal.Zero, money.ErrInvalidAmount
	}
	return amount.Decimal(), nil
}
