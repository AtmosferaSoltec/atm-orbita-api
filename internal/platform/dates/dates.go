// Package dates maneja fechas sin hora ni zona (YYYY-MM-DD), como las columnas
// DATE, y calcula "hoy" según la zona horaria de una persona.
package dates

import (
	"errors"
	"fmt"
	"time"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
)

const layout = "2006-01-02"

// ErrInvalidDate: el texto no es una fecha real en formato YYYY-MM-DD.
var ErrInvalidDate = errors.New("invalid date")

// LocalDate es un día del calendario. Se guarda como medianoche UTC solo para
// poder compararlo; nunca representa un instante.
type LocalDate struct {
	t time.Time
}

// New construye una fecha a partir de año, mes y día.
func New(year int, month time.Month, day int) LocalDate {
	return LocalDate{t: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// FromTime toma el día de t tal como se ve en su propia zona.
func FromTime(t time.Time) LocalDate {
	return New(t.Year(), t.Month(), t.Day())
}

// Parse acepta solo YYYY-MM-DD y fechas que existen (rechaza 2026-02-30).
func Parse(text string) (LocalDate, error) {
	t, err := time.Parse(layout, text)
	if err != nil {
		return LocalDate{}, fmt.Errorf("%w: %q", ErrInvalidDate, text)
	}
	return LocalDate{t: t}, nil
}

// Today es el día de hoy en la zona IANA dada. Un servidor en UTC ya está en
// "mañana" desde las 19:00 de Lima: por eso nunca se usa la fecha del servidor.
func Today(c clock.Clock, location *time.Location) LocalDate {
	return FromTime(c.Now().In(location))
}

// LoadLocation valida una zona IANA ("America/Lima").
func LoadLocation(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, fmt.Errorf("invalid time zone %q", name)
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("invalid time zone %q: %w", name, err)
	}
	return location, nil
}

// Time devuelve la fecha como medianoche UTC, para guardarla en una columna DATE.
func (d LocalDate) Time() time.Time { return d.t }

// IsZero dice si la fecha no se fijó.
func (d LocalDate) IsZero() bool { return d.t.IsZero() }

// Before y After comparan días.
func (d LocalDate) Before(other LocalDate) bool { return d.t.Before(other.t) }

// After dice si d es posterior a other.
func (d LocalDate) After(other LocalDate) bool { return d.t.After(other.t) }

// AddDays suma (o resta) días.
func (d LocalDate) AddDays(days int) LocalDate { return LocalDate{t: d.t.AddDate(0, 0, days)} }

// String da la fecha en YYYY-MM-DD.
func (d LocalDate) String() string { return d.t.Format(layout) }

// MarshalJSON escribe "YYYY-MM-DD".
func (d LocalDate) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON lee "YYYY-MM-DD".
func (d *LocalDate) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("%w: %s", ErrInvalidDate, data)
	}
	parsed, err := Parse(string(data[1 : len(data)-1]))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
