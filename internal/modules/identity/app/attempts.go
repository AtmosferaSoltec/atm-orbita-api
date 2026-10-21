package app

import (
	"sync"
	"time"
)

// Tras failuresBeforeWait fallos seguidos con un correo, se espera cada vez más
// en lugar de bloquear la cuenta: así nadie puede dejar fuera a otra persona a
// propósito.
const failuresBeforeWait = 5

var waits = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

const (
	maxTrackedEmails = 10_000
	forgetAfter      = 30 * time.Minute
)

// loginAttempts cuenta los fallos por correo, en memoria (una sola réplica).
type loginAttempts struct {
	mu       sync.Mutex
	failures map[string]*failures
}

type failures struct {
	count      int
	last       time.Time
	blockedTil time.Time
}

func newLoginAttempts() *loginAttempts {
	return &loginAttempts{failures: map[string]*failures{}}
}

// wait dice cuánto falta para poder volver a intentar con email.
func (a *loginAttempts) wait(email string, now time.Time) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	if f, ok := a.failures[email]; ok && now.Before(f.blockedTil) {
		return f.blockedTil.Sub(now)
	}
	return 0
}

func (a *loginAttempts) failed(email string, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Quien prueba miles de correos al azar no debe llenar la memoria.
	if len(a.failures) > maxTrackedEmails {
		for key, old := range a.failures {
			if now.Sub(old.last) > forgetAfter && now.After(old.blockedTil) {
				delete(a.failures, key)
			}
		}
	}
	f, ok := a.failures[email]
	if !ok {
		f = &failures{}
		a.failures[email] = f
	}
	f.count++
	f.last = now
	if f.count >= failuresBeforeWait {
		step := min(f.count-failuresBeforeWait, len(waits)-1)
		f.blockedTil = now.Add(waits[step])
	}
}

func (a *loginAttempts) succeeded(email string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.failures, email)
}
