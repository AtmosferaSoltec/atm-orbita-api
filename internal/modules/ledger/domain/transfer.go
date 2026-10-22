package domain

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Códigos de campo de las transferencias (ORBITA_SPEC §4.1).
const (
	CodeSameAccount      = "SAME_ACCOUNT"
	CodeRateRequired     = "RATE_REQUIRED"
	CodeAmountsMustMatch = "AMOUNTS_MUST_MATCH"
)

// Transfer mueve dinero entre dos cuentas propias. No es ingreso ni gasto. Lo
// que mueve los saldos son los dos montos; ExchangeRate (unidades de la moneda
// de destino por 1 de la de origen) es informativo y nil en la misma moneda.
type Transfer struct {
	ID            uuid.UUID
	FromAccountID uuid.UUID
	ToAccountID   uuid.UUID
	FromAmount    decimal.Decimal
	ToAmount      decimal.Decimal
	ExchangeRate  *decimal.Decimal
	OccurredOn    dates.LocalDate
	Note          string
}

// TransferEntry es una transferencia en la lista, con sus dos cuentas.
type TransferEntry struct {
	Transfer
	From EntryAccount
	To   EntryAccount
}

// ListEntry es una fila de la lista: un movimiento o una transferencia.
type ListEntry struct {
	Movement *Entry
	Transfer *TransferEntry
}

// CheckTransferRate aplica las reglas de moneda, ya conocidas las de las dos
// cuentas: en la misma moneda no hay tipo de cambio y los montos son iguales;
// entre monedas distintas el tipo de cambio es obligatorio y mayor que cero (el
// API no lo recalcula: guarda el que manda la app). amountsOK dice si los dos
// montos se leyeron bien.
func CheckTransferRate(fromCurrency, toCurrency string, from, to decimal.Decimal, amountsOK bool, rate *string, problems *ValidationError) *decimal.Decimal {
	if fromCurrency == toCurrency {
		if rate != nil || (amountsOK && !from.Equal(to)) {
			problems.Add("to_amount", CodeAmountsMustMatch)
		}
		return nil
	}
	if rate == nil {
		problems.Add("exchange_rate", CodeRateRequired)
		return nil
	}
	parsed, err := money.ParseRate(*rate)
	if err != nil || !parsed.IsPositive() {
		problems.Add("exchange_rate", CodeRateRequired)
		return nil
	}
	value := parsed.Decimal()
	return &value
}

// SameTransfer compara un reintento; sin fecha en la petición, la fecha no cuenta.
func SameTransfer(a, b Transfer, compareDate bool) bool {
	sameRate := (a.ExchangeRate == nil) == (b.ExchangeRate == nil) &&
		(a.ExchangeRate == nil || a.ExchangeRate.Equal(*b.ExchangeRate))
	return a.FromAccountID == b.FromAccountID && a.ToAccountID == b.ToAccountID &&
		a.FromAmount.Equal(b.FromAmount) && a.ToAmount.Equal(b.ToAmount) && sameRate &&
		a.Note == b.Note && (!compareDate || a.OccurredOn == b.OccurredOn)
}
