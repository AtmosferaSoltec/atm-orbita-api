package domain

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Códigos de campo de los movimientos y de la lista (ORBITA_SPEC §4.1).
const (
	CodeAmountRequired     = "AMOUNT_REQUIRED"
	CodeAccountRequired    = "ACCOUNT_REQUIRED"
	CodeCategoryRequired   = "CATEGORY_REQUIRED"
	CodeDescriptionTooLong = "DESCRIPTION_TOO_LONG"
	CodeFutureDate         = "FUTURE_DATE"
	CodeRangeInvalid       = "RANGE_INVALID"
)

// DescriptionMaxLength es el largo máximo de una descripción, en caracteres.
const DescriptionMaxLength = 500

// MaxRangeYears es el periodo más largo que se puede listar de una vez.
const MaxRangeYears = 10

var (
	ErrAccountArchived  = errors.New("account archived")
	ErrCategoryArchived = errors.New("category archived")
	// ErrPaymentExpenseLocked: cambiar la categoría o la descripción del egreso de un pago.
	ErrPaymentExpenseLocked = errors.New("payment expense locked")
)

// Transaction es un ingreso o un egreso. Su moneda es la de su cuenta y su
// tipo, el de su categoría.
type Transaction struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	CategoryID  uuid.UUID
	Kind        Kind
	Amount      decimal.Decimal
	OccurredOn  dates.LocalDate
	Description string
	// CreditPurchaseID es la compra con tarjeta que pagó este egreso (plan 10), o nil.
	CreditPurchaseID *uuid.UUID
}

// EntryAccount es lo que una fila de la lista muestra de su cuenta.
type EntryAccount struct {
	ID       uuid.UUID
	Name     string
	Type     AccountType
	Currency string
}

// Entry es una fila de la lista de movimientos, con su cuenta y su categoría.
type Entry struct {
	Transaction
	Account  EntryAccount
	Category Category
}

// ValidateAmount lee un monto obligatorio y mayor que cero, con hasta 2 decimales.
func ValidateAmount(text *string, problems *ValidationError) decimal.Decimal {
	value, _ := ValidateAmountField("amount", text, problems)
	return value
}

// ValidateAmountField es ValidateAmount para el campo field; ok dice si es válido.
func ValidateAmountField(field string, text *string, problems *ValidationError) (decimal.Decimal, bool) {
	if text == nil {
		problems.Add(field, CodeAmountRequired)
		return decimal.Zero, false
	}
	amount, err := money.ParseAmount(*text)
	if err != nil || !amount.IsPositive() {
		problems.Add(field, CodeAmountRequired)
		return decimal.Zero, false
	}
	return amount.Decimal(), true
}

// ValidateDescription recorta la descripción y revisa su largo en caracteres.
func ValidateDescription(text string, problems *ValidationError) string {
	return ValidateTextField("description", text, problems)
}

// ValidateTextField es ValidateDescription para el campo field (la nota de una
// transferencia usa el mismo código).
func ValidateTextField(field, text string, problems *ValidationError) string {
	clean := strings.TrimSpace(text)
	if utf8.RuneCountInString(clean) > DescriptionMaxLength {
		problems.Add(field, CodeDescriptionTooLong)
	}
	return clean
}

// ValidateDate lee la fecha: sin ella es hoy; nunca posterior a hoy (en la zona
// del perfil) y sin tope hacia atrás.
func ValidateDate(text *string, today dates.LocalDate, problems *ValidationError) dates.LocalDate {
	if text == nil {
		return today
	}
	day, err := dates.Parse(*text)
	if err != nil {
		problems.Add("occurred_on", CodeInvalidFormat)
		return today
	}
	if day.After(today) {
		problems.Add("occurred_on", CodeFutureDate)
	}
	return day
}

// ValidateRange lee el periodo de la lista: las dos fechas son obligatorias,
// from no es posterior a to y no abarca más de 10 años.
func ValidateRange(from, to string, problems *ValidationError) (dates.LocalDate, dates.LocalDate) {
	start, errFrom := dates.Parse(from)
	end, errTo := dates.Parse(to)
	if errFrom != nil {
		problems.Add("from", CodeInvalidFormat)
	}
	if errTo != nil {
		problems.Add("to", CodeInvalidFormat)
	}
	if errFrom != nil || errTo != nil {
		return start, end
	}
	limit := dates.FromTime(start.Time().AddDate(MaxRangeYears, 0, 0))
	if start.After(end) || !end.Before(limit) {
		problems.Add("to", CodeRangeInvalid)
	}
	return start, end
}

// SearchPattern convierte lo que se busca en un patrón ILIKE que lo contiene,
// escapando %, _ y \ para que se busquen tal cual. Vacío es nil: no filtra.
func SearchPattern(q string) *string {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	pattern := "%" + escaped + "%"
	return &pattern
}
