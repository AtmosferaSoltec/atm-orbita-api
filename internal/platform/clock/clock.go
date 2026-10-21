// Package clock da la hora actual detrás de una interfaz, para que las pruebas
// puedan fijar "ahora" (y con ello "hoy") sin depender del reloj del sistema.
package clock

import "time"

// Clock devuelve el instante actual.
type Clock interface {
	Now() time.Time
}

// System es el reloj real, siempre en UTC.
type System struct{}

// Now devuelve la hora del sistema en UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Fixed es un reloj detenido en un instante, para pruebas.
type Fixed struct {
	At time.Time
}

// Now devuelve siempre el mismo instante.
func (f Fixed) Now() time.Time { return f.At }
