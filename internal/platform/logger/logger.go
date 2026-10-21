// Package logger arma el slog de la app y lo lleva en el contexto de cada
// petición, con su request_id, para que todas las líneas de una petición se
// puedan encontrar juntas.
package logger

import (
	"context"
	"io"
	"log/slog"
)

type contextKey struct{}

// New crea el logger: JSON en producción (lo leen las herramientas del
// servidor) y texto en desarrollo (lo lee una persona).
func New(w io.Writer, production bool) *slog.Logger {
	options := &slog.HandlerOptions{Level: slog.LevelInfo}
	if production {
		return slog.New(slog.NewJSONHandler(w, options))
	}
	options.Level = slog.LevelDebug
	return slog.New(slog.NewTextHandler(w, options))
}

// WithContext guarda el logger de la petición.
func WithContext(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

// From devuelve el logger de la petición, o el global si no hay uno.
func From(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return log
	}
	return slog.Default()
}
