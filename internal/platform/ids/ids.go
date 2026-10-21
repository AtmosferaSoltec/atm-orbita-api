// Package ids genera y valida identificadores.
package ids

import (
	"fmt"

	"github.com/google/uuid"
)

// New genera un UUID v7: ordenable por tiempo, lo que mantiene compactos los
// índices de las tablas que crecen.
func New() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// Parse acepta cualquier UUID válido. Las apps generan los suyos (Android usa
// v4) y el API los respeta al crear, para que reintentar no duplique.
func Parse(text string) (uuid.UUID, error) {
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid id %q: %w", text, err)
	}
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("invalid id %q: nil uuid", text)
	}
	return id, nil
}
