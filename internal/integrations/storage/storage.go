// Package storage guarda archivos fuera de la base (Cloudflare R2). Los módulos
// dependen del puerto Storage; main elige la implementación: R2 si hay R2_*,
// Unavailable si no (pruebas, desarrollo sin credenciales) y Memory en pruebas.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrUnavailable: no hay almacenamiento configurado.
var ErrUnavailable = errors.New("storage is not configured")

// ErrNotFound: la llave no existe.
var ErrNotFound = errors.New("object not found")

// GetOptions ajusta cómo entrega el archivo una URL firmada.
type GetOptions struct {
	FileName    string
	ContentType string
	// Download pide al navegador guardarlo en vez de mostrarlo.
	Download bool
}

// Storage es el puerto que usan los módulos.
type Storage interface {
	Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error
	// Get abre el objeto; quien llama lo cierra. ErrNotFound si no existe.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete borra las llaves; las que no existen se ignoran.
	Delete(ctx context.Context, keys ...string) error
	// DeletePrefix borra todo lo que empieza con prefix (al eliminar una cuenta).
	DeletePrefix(ctx context.Context, prefix string) error
	// PresignGet da una URL de descarga que vence en ttl. Los archivos nunca
	// pasan por el API al bajarlos (AGENT_GUIDE §9).
	PresignGet(ctx context.Context, key string, ttl time.Duration, opts GetOptions) (string, error)
}

// Unavailable responde ErrUnavailable a todo.
type Unavailable struct{}

func (Unavailable) Put(context.Context, string, string, io.Reader, int64) error {
	return ErrUnavailable
}
func (Unavailable) Get(context.Context, string) (io.ReadCloser, error) { return nil, ErrUnavailable }
func (Unavailable) Delete(context.Context, ...string) error            { return ErrUnavailable }
func (Unavailable) DeletePrefix(context.Context, string) error         { return ErrUnavailable }
func (Unavailable) PresignGet(context.Context, string, time.Duration, GetOptions) (string, error) {
	return "", ErrUnavailable
}
