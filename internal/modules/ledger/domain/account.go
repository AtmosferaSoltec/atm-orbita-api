package domain

import (
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/currency"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// AccountType es la clase de una cuenta.
type AccountType string

// Tipos de cuenta.
const (
	AccountCash    AccountType = "cash"
	AccountDebit   AccountType = "debit"
	AccountSavings AccountType = "savings"
	AccountOther   AccountType = "other"
)

// Códigos de campo de las cuentas.
const (
	CodeTypeInvalid          = "TYPE_INVALID"
	CodeCurrencyNotSupported = "CURRENCY_NOT_SUPPORTED"
	CodeAmountInvalid        = "AMOUNT_INVALID"
	CodeFieldNotEditable     = "FIELD_NOT_EDITABLE"
)

// ErrFxNotConfigured: una cuenta en una moneda distinta de la principal antes de
// que exista el tipo de cambio.
var ErrFxNotConfigured = errors.New("exchange rate not configured")

// InitialAccountName es la cuenta con que nace todo usuario.
const InitialAccountName = "Efectivo"

// Account es un lugar donde hay dinero, en una sola moneda. Balance no se guarda:
// se calcula (saldo inicial + ingresos − egresos + transferencias).
type Account struct {
	ID               uuid.UUID
	Name             string
	Type             AccountType
	Currency         string
	Balance          decimal.Decimal
	IncludeInSavings bool
}

// ValidateAccountType revisa que sea uno de los cuatro tipos.
func ValidateAccountType(t AccountType, problems *ValidationError) {
	switch t {
	case AccountCash, AccountDebit, AccountSavings, AccountOther:
	default:
		problems.Add("type", CodeTypeInvalid)
	}
}

// ValidateCurrency revisa que la moneda esté en el catálogo.
func ValidateCurrency(code string, problems *ValidationError) {
	if !currency.IsSupported(code) {
		problems.Add("currency", CodeCurrencyNotSupported)
	}
}

// ValidateInitialBalance lee el saldo inicial: ausente es 0; si viene, texto con
// hasta 2 decimales y nunca negativo.
func ValidateInitialBalance(text *string, problems *ValidationError) decimal.Decimal {
	if text == nil {
		return decimal.Zero
	}
	amount, err := money.ParseAmount(*text)
	if err != nil {
		problems.Add("initial_balance", CodeAmountInvalid)
		return decimal.Zero
	}
	return amount.Decimal()
}

// Fx son las dos monedas del usuario y el tipo de cambio entre ellas (unidades de
// Main por 1 de Secondary), o nil si todavía no lo escribió. Es la copia del
// perfil que necesita el libro.
type Fx struct {
	Main      string
	Secondary string
	Rate      *decimal.Decimal
}

// conversionPlaces son los decimales de una división intermedia (FxPair.kt).
const conversionPlaces = 10

// RequireUsable: sin tipo de cambio, solo se usa la moneda principal.
func (f Fx) RequireUsable(code string) error {
	if code != f.Main && f.Rate == nil {
		return ErrFxNotConfigured
	}
	return nil
}

// raw convierte sin redondear; ok es false si no hay tipo de cambio para eso.
func (f Fx) raw(amount decimal.Decimal, from, to string) (decimal.Decimal, bool) {
	switch {
	case from == to:
		return amount, true
	case f.Rate == nil:
		return decimal.Decimal{}, false
	case from == f.Secondary && to == f.Main:
		return amount.Mul(*f.Rate), true
	case from == f.Main && to == f.Secondary:
		return amount.DivRound(*f.Rate, conversionPlaces), true
	default:
		return decimal.Decimal{}, false
	}
}

// CurrencyAmount es un total en una moneda.
type CurrencyAmount struct {
	Currency string
	Amount   decimal.Decimal
}

// Savings es el ahorro total: cuántas cuentas cuentan y el total en cada moneda
// que se puede mostrar (las dos del par con tipo de cambio; solo la principal sin él).
type Savings struct {
	IncludedCount int
	ActiveCount   int
	Totals        []CurrencyAmount
}

// ComputeSavings suma las cuentas incluidas convertidas a cada moneda y redondea
// una sola vez al final (HALF_UP), como FxPair.savingsTotal. Una cuenta que no se
// puede convertir (moneda fuera del par) no suma.
func ComputeSavings(accounts []Account, fx Fx) Savings {
	out := Savings{ActiveCount: len(accounts)}
	for _, a := range accounts {
		if a.IncludeInSavings {
			out.IncludedCount++
		}
	}
	targets := []string{fx.Main}
	if fx.Rate != nil {
		targets = append(targets, fx.Secondary)
	}
	for _, target := range targets {
		total := decimal.Zero
		for _, a := range accounts {
			if !a.IncludeInSavings {
				continue
			}
			if value, ok := fx.raw(a.Balance, a.Currency, target); ok {
				total = total.Add(value)
			}
		}
		out.Totals = append(out.Totals, CurrencyAmount{Currency: target, Amount: total.Round(money.AmountScale)})
	}
	return out
}
