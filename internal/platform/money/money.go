// Package money representa montos y tipos de cambio con decimales exactos.
//
// En JSON viajan siempre como texto ("1245.80"), nunca como número: así ninguna
// plataforma los pasa por coma flotante. Nunca se usa float64 para dinero.
package money

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/shopspring/decimal"
)

// AmountScale y RateScale son los decimales que se guardan (NUMERIC(14,2) y NUMERIC(18,6)).
const (
	AmountScale = 2
	RateScale   = 6
)

var (
	// Hasta 12 enteros caben en NUMERIC(14,2). Ni coma, ni signo, ni exponente:
	// la app normaliza antes de enviar.
	amountPattern = regexp.MustCompile(`^\d{1,12}(\.\d{1,2})?$`)
	ratePattern   = regexp.MustCompile(`^\d{1,12}(\.\d{1,6})?$`)

	// ErrInvalidAmount: el texto no es un monto con el formato del API.
	ErrInvalidAmount = errors.New("invalid amount")
	// ErrInvalidRate: el texto no es un tipo de cambio con el formato del API.
	ErrInvalidRate = errors.New("invalid exchange rate")
)

// Amount es un monto con 2 decimales.
type Amount struct {
	value decimal.Decimal
}

// NewAmount redondea a 2 decimales con HALF_UP (redondeo de Android: una sola vez, al final).
func NewAmount(value decimal.Decimal) Amount {
	return Amount{value: roundHalfUp(value, AmountScale)}
}

// ParseAmount acepta solo el formato del API. No comprueba que sea mayor que
// cero: eso es una regla de cada caso de uso.
func ParseAmount(text string) (Amount, error) {
	if !amountPattern.MatchString(text) {
		return Amount{}, fmt.Errorf("%w: %q", ErrInvalidAmount, text)
	}
	return Amount{value: decimal.RequireFromString(text)}, nil
}

// Decimal devuelve el valor para operar.
func (a Amount) Decimal() decimal.Decimal { return a.value }

// IsPositive dice si es mayor que cero.
func (a Amount) IsPositive() bool { return a.value.IsPositive() }

// String da el monto con exactamente 2 decimales: "1245.80".
func (a Amount) String() string { return a.value.StringFixed(AmountScale) }

// MarshalJSON escribe el monto como texto.
func (a Amount) MarshalJSON() ([]byte, error) {
	return []byte(`"` + a.String() + `"`), nil
}

// UnmarshalJSON solo acepta un texto con el formato del API; un número JSON es un error.
func (a *Amount) UnmarshalJSON(data []byte) error {
	text, err := unquote(data)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidAmount, data)
	}
	parsed, err := ParseAmount(text)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// Rate es un tipo de cambio con hasta 6 decimales.
type Rate struct {
	value decimal.Decimal
}

// NewRate redondea a 6 decimales con HALF_UP.
func NewRate(value decimal.Decimal) Rate {
	return Rate{value: roundHalfUp(value, RateScale)}
}

// ParseRate acepta solo el formato del API.
func ParseRate(text string) (Rate, error) {
	if !ratePattern.MatchString(text) {
		return Rate{}, fmt.Errorf("%w: %q", ErrInvalidRate, text)
	}
	return Rate{value: decimal.RequireFromString(text)}, nil
}

// Decimal devuelve el valor para operar.
func (r Rate) Decimal() decimal.Decimal { return r.value }

// IsPositive dice si es mayor que cero.
func (r Rate) IsPositive() bool { return r.value.IsPositive() }

// String da el tipo de cambio con 6 decimales: "3.200000".
func (r Rate) String() string { return r.value.StringFixed(RateScale) }

// MarshalJSON escribe el tipo de cambio como texto.
func (r Rate) MarshalJSON() ([]byte, error) {
	return []byte(`"` + r.String() + `"`), nil
}

// UnmarshalJSON solo acepta un texto con el formato del API.
func (r *Rate) UnmarshalJSON(data []byte) error {
	text, err := unquote(data)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidRate, data)
	}
	parsed, err := ParseRate(text)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// roundHalfUp redondea alejándose de cero en el medio, como RoundingMode.HALF_UP
// de Java. decimal.Round hace eso mismo.
func roundHalfUp(value decimal.Decimal, places int32) decimal.Decimal {
	return value.Round(places)
}

func unquote(data []byte) (string, error) {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return "", errors.New("not a JSON string")
	}
	return string(data[1 : len(data)-1]), nil
}
