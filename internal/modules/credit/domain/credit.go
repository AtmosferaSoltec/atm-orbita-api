// Package domain son las reglas de las tarjetas de crédito y sus compras
// pendientes. Go puro, sin Gin ni Postgres.
package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/currency"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/text"
)

// Códigos de campo (los del enum ValidationError de Android cuando existen).
const (
	CodeNameRequired         = "NAME_REQUIRED"
	CodeNameTooLong          = "NAME_TOO_LONG"
	CodeCurrencyNotSupported = "CURRENCY_NOT_SUPPORTED"
	CodeAmountRequired       = "AMOUNT_REQUIRED"
	CodeCardRequired         = "CARD_REQUIRED"
	CodeCategoryRequired     = "CATEGORY_REQUIRED"
	CodeCategoryKindMismatch = "CATEGORY_KIND_MISMATCH"
	CodeDescriptionTooLong   = "DESCRIPTION_TOO_LONG"
	CodeDueDatePast          = "DUE_DATE_PAST"
	CodeInvalidFormat        = "INVALID_FORMAT"
)

// Largos máximos, en caracteres.
const (
	NameMaxLength        = 60
	DescriptionMaxLength = 500
)

// UrgentDays: una compra que vence en 3 días o menos, o ya venció, es urgente.
const UrgentDays = 3

// Estados de una compra.
const (
	StatusPending = "pending"
	StatusPaid    = "paid"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrIDTaken             = errors.New("id already used with other content")
	ErrFxNotConfigured     = errors.New("exchange rate not configured")
	ErrCardArchived        = errors.New("card archived")
	ErrCardHasPending      = errors.New("card has pending purchases")
	ErrPurchaseAlreadyPaid = errors.New("purchase already paid")
	ErrCategoryArchived    = errors.New("category archived")
)

// FieldError es un campo con problema.
type FieldError struct {
	Field string
	Code  string
}

// ValidationError junta los problemas de un formulario, en el orden de Android.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string { return fmt.Sprintf("validation failed: %v", e.Fields) }

// Add agrega un problema.
func (e *ValidationError) Add(field, code string) {
	e.Fields = append(e.Fields, FieldError{Field: field, Code: code})
}

// OrNil devuelve el error solo si hay problemas.
func (e *ValidationError) OrNil() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

// Card es una tarjeta de crédito: agrupa compras pendientes. Las nuevas toman
// su moneda.
type Card struct {
	ID       uuid.UUID
	Name     string
	Currency string
}

// Category es lo que una compra muestra de su categoría (de ledger).
type Category struct {
	ID    uuid.UUID
	Name  string
	Kind  string
	Color string
}

// Purchase es una compra con tarjeta. Pendiente no es un movimiento: no toca
// saldos ni reportes hasta que se paga.
type Purchase struct {
	ID           uuid.UUID
	CardID       uuid.UUID
	CategoryID   uuid.UUID
	Description  string
	Amount       decimal.Decimal
	Currency     string
	PurchaseDate dates.LocalDate
	DueDate      dates.LocalDate
}

// IsUrgent dice si vence en 3 días o menos, o ya venció.
func (p Purchase) IsUrgent(today dates.LocalDate) bool {
	return !p.DueDate.After(today.AddDays(UrgentDays))
}

// ValidateCardName limpia el nombre y revisa su largo.
func ValidateCardName(name string, problems *ValidationError) string {
	clean := text.CleanName(name)
	switch {
	case clean == "":
		problems.Add("name", CodeNameRequired)
	case utf8.RuneCountInString(clean) > NameMaxLength:
		problems.Add("name", CodeNameTooLong)
	}
	return clean
}

// ValidateCurrency revisa que la moneda esté en el catálogo.
func ValidateCurrency(code string, problems *ValidationError) {
	if !currency.IsSupported(code) {
		problems.Add("currency", CodeCurrencyNotSupported)
	}
}

// ValidateAmount lee un monto obligatorio y mayor que cero.
func ValidateAmount(value *string, problems *ValidationError) decimal.Decimal {
	if value == nil {
		problems.Add("amount", CodeAmountRequired)
		return decimal.Zero
	}
	amount, err := money.ParseAmount(*value)
	if err != nil || !amount.IsPositive() {
		problems.Add("amount", CodeAmountRequired)
		return decimal.Zero
	}
	return amount.Decimal()
}

// ValidateDescription recorta la descripción y revisa su largo.
func ValidateDescription(value string, problems *ValidationError) string {
	clean := strings.TrimSpace(value)
	if utf8.RuneCountInString(clean) > DescriptionMaxLength {
		problems.Add("description", CodeDescriptionTooLong)
	}
	return clean
}

// ValidateDueDate lee la fecha límite: obligatoria y no anterior a notBefore
// (hoy al crear; la fecha de compra al editar, para poder corregir una vencida).
func ValidateDueDate(value *string, notBefore dates.LocalDate, problems *ValidationError) dates.LocalDate {
	if value == nil {
		problems.Add("due_date", CodeInvalidFormat)
		return dates.LocalDate{}
	}
	day, err := dates.Parse(*value)
	if err != nil {
		problems.Add("due_date", CodeInvalidFormat)
		return dates.LocalDate{}
	}
	if day.Before(notBefore) {
		problems.Add("due_date", CodeDueDatePast)
	}
	return day
}

// CurrencyAmount es un monto en una moneda.
type CurrencyAmount struct {
	Currency string
	Amount   decimal.Decimal
}

// Debt suma las compras por moneda, sin mezclar; la principal primero, luego
// las demás por código.
func Debt(purchases []Purchase, mainCurrency string) []CurrencyAmount {
	totals := map[string]decimal.Decimal{}
	for _, p := range purchases {
		totals[p.Currency] = totals[p.Currency].Add(p.Amount)
	}
	out := make([]CurrencyAmount, 0, len(totals))
	for code, amount := range totals {
		out = append(out, CurrencyAmount{Currency: code, Amount: amount})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Currency == mainCurrency) != (out[j].Currency == mainCurrency) {
			return out[i].Currency == mainCurrency
		}
		return out[i].Currency < out[j].Currency
	})
	return out
}

// CardWithDebt es una tarjeta con lo que debe.
type CardWithDebt struct {
	Card
	Debt         []CurrencyAmount
	PendingCount int
	NextDueDate  *dates.LocalDate
}

// CardsWithDebt arma la deuda de cada tarjeta y las ordena: la de fecha límite
// más próxima primero; las que no deben nada al final, en el orden en que llegan.
func CardsWithDebt(cards []Card, pending []Purchase, mainCurrency string) []CardWithDebt {
	byCard := map[uuid.UUID][]Purchase{}
	for _, p := range pending {
		byCard[p.CardID] = append(byCard[p.CardID], p)
	}
	out := make([]CardWithDebt, 0, len(cards))
	for _, card := range cards {
		own := byCard[card.ID]
		item := CardWithDebt{Card: card, Debt: Debt(own, mainCurrency), PendingCount: len(own)}
		for _, p := range own {
			if item.NextDueDate == nil || p.DueDate.Before(*item.NextDueDate) {
				due := p.DueDate
				item.NextDueDate = &due
			}
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].NextDueDate, out[j].NextDueDate
		switch {
		case a == nil || b == nil:
			return a != nil && b == nil
		default:
			return a.Before(*b)
		}
	})
	return out
}

// Summary es el aviso de tarjetas de Inicio.
type Summary struct {
	PendingCount int
	NextDueDate  dates.LocalDate
	HasUrgent    bool
}

// Summarize resume las compras pendientes; ok es false si no hay ninguna.
func Summarize(pending []Purchase, today dates.LocalDate) (Summary, bool) {
	if len(pending) == 0 {
		return Summary{}, false
	}
	s := Summary{PendingCount: len(pending), NextDueDate: pending[0].DueDate}
	for _, p := range pending {
		if p.DueDate.Before(s.NextDueDate) {
			s.NextDueDate = p.DueDate
		}
		if p.IsUrgent(today) {
			s.HasUrgent = true
		}
	}
	return s, true
}
