// Package domain son las preferencias del usuario: sus dos monedas, en cuál ve
// el ahorro, su zona horaria y el tipo de cambio entre las dos, que puede no
// existir. Es el mismo modelo que FxPair de Android (domain/model/FxPair.kt).
package domain

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// Cuando la moneda principal ya es dólares, la secundaria nace en soles.
const (
	DefaultSecondary             = "USD"
	SecondaryWhenMainIsUSD       = "PEN"
	FxModeManual                 = "manual"
	conversionPlaces             = 10
	rateScale              int32 = 6
)

// Códigos de campo.
const (
	CodeCurrencyNotSupported = "CURRENCY_NOT_SUPPORTED"
	CodeSameCurrency         = "SAME_CURRENCY"
	CodeRateRequired         = "RATE_REQUIRED"
	CodeCurrencyNotInPair    = "CURRENCY_NOT_IN_PAIR"
	CodeTimezoneInvalid      = "TIMEZONE_INVALID"
	CodeInvalidFormat        = "INVALID_FORMAT"
)

var (
	// ErrFxNotConfigured: algo en una moneda distinta de la principal antes de
	// configurar el tipo de cambio.
	ErrFxNotConfigured = errors.New("exchange rate not configured")
	ErrNotFound        = errors.New("profile not found")
)

// FieldError es un campo con problema.
type FieldError struct {
	Field string
	Code  string
}

// ValidationError junta los problemas de un formulario, en orden.
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

// SecondaryFor es la moneda secundaria con la que nace un perfil.
func SecondaryFor(main string) string {
	if main == DefaultSecondary {
		return SecondaryWhenMainIsUSD
	}
	return DefaultSecondary
}

// FxPair son las dos monedas y el tipo de cambio entre ellas: Rate son unidades
// de Main por 1 de Secondary (1 US$ = S/ 3.20). Rate es nil hasta que la
// persona lo escribe: nunca se inventa un valor.
type FxPair struct {
	Main      string
	Secondary string
	Rate      *decimal.Decimal
}

// IsConfigured dice si hay tipo de cambio.
func (p FxPair) IsConfigured() bool { return p.Rate != nil }

// raw convierte sin redondear; ok es false si no hay tipo de cambio para eso.
func (p FxPair) raw(amount decimal.Decimal, from, to string) (decimal.Decimal, bool) {
	switch {
	case from == to:
		return amount, true
	case p.Rate == nil:
		return decimal.Decimal{}, false
	case from == p.Secondary && to == p.Main:
		return amount.Mul(*p.Rate), true
	case from == p.Main && to == p.Secondary:
		return amount.DivRound(*p.Rate, conversionPlaces), true
	default:
		return decimal.Decimal{}, false
	}
}

// Convert redondea una sola vez, al final, a 2 decimales (HALF_UP).
func (p FxPair) Convert(amount decimal.Decimal, from, to string) (decimal.Decimal, bool) {
	value, ok := p.raw(amount, from, to)
	if !ok {
		return decimal.Decimal{}, false
	}
	return value.Round(2), true
}

// RateBetween son unidades de to por 1 de from, con 6 decimales.
func (p FxPair) RateBetween(from, to string) (decimal.Decimal, bool) {
	value, ok := p.raw(decimal.NewFromInt(1), from, to)
	if !ok {
		return decimal.Decimal{}, false
	}
	return value.Round(rateScale), true
}

// Inverse es 1 ÷ rate con 6 decimales: el valor de 1 Main en Secondary.
func Inverse(rate decimal.Decimal) decimal.Decimal {
	return decimal.NewFromInt(1).DivRound(rate, rateScale)
}

// Profile son las preferencias guardadas.
type Profile struct {
	FxPair
	DisplayCurrency string
	Timezone        string
	FxMode          string
}

// UsableCurrency dice si se puede operar en currency: sin tipo de cambio, solo
// en la principal. Así ningún total queda a medias.
func (p Profile) UsableCurrency(currency string) error {
	if currency != p.Main && !p.IsConfigured() {
		return ErrFxNotConfigured
	}
	return nil
}

// DisplayAfterPairChange es la moneda de visualización tras cambiar el par: si
// la que tenía ya no está en el par, pasa a la principal.
func DisplayAfterPairChange(current, main, secondary string) string {
	if current == main || current == secondary {
		return current
	}
	return main
}
