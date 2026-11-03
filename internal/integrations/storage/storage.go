// Package storage guarda archivos fuera de la base. Los módulos dependen del
// puerto Storage; main elige la implementación: StorageAPI (atm-storage-api) si
// hay AUTH_CLIENT_SECRET, Unavailable si no, y Memory en pruebas.
package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

// ErrUnavailable: no hay almacenamiento configurado.
var ErrUnavailable = errors.New("storage is not configured")

// ErrNotFound: el archivo no existe.
var ErrNotFound = errors.New("object not found")

// GetOptions ajusta cómo entrega el archivo una URL firmada.
type GetOptions struct {
	FileName    string
	ContentType string
	// Download pide al navegador guardarlo en vez de mostrarlo. atm-storage-api
	// siempre entrega como descarga, así que StorageAPI lo ignora.
	Download bool
}

// Storage es el puerto que usan los módulos. La referencia de un archivo es el
// texto que devuelve Put: la genera el almacenamiento, no quien llama.
type Storage interface {
	// Put guarda el archivo a nombre de ownerID (en atm-storage-api va como
	// company_id: Orbita no tiene empresas, cada persona es la suya) y devuelve su
	// referencia.
	Put(ctx context.Context, ownerID uuid.UUID, fileName, contentType string, body io.Reader, size int64) (string, error)
	// Get abre el archivo; quien llama lo cierra. ErrNotFound si no existe.
	Get(ctx context.Context, ref string) (io.ReadCloser, error)
	// Delete borra los archivos; los que ya no existen se ignoran. Intenta con
	// todos aunque alguno falle.
	Delete(ctx context.Context, refs ...string) error
	// PresignGet da una URL de descarga de corta vida. Los archivos nunca pasan
	// por el API al bajarlos (AGENT_GUIDE §9). ttl es una referencia: el
	// almacenamiento fija el vencimiento (unos 5 minutos).
	PresignGet(ctx context.Context, ref string, ttl time.Duration, opts GetOptions) (string, error)
}

// Unavailable responde ErrUnavailable a todo.
type Unavailable struct{}

func (Unavailable) Put(context.Context, uuid.UUID, string, string, io.Reader, int64) (string, error) {
	return "", ErrUnavailable
}
func (Unavailable) Get(context.Context, string) (io.ReadCloser, error) { return nil, ErrUnavailable }
func (Unavailable) Delete(context.Context, ...string) error            { return ErrUnavailable }
func (Unavailable) PresignGet(context.Context, string, time.Duration, GetOptions) (string, error) {
	return "", ErrUnavailable
}
