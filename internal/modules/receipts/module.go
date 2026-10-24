// Package receipts son los recibos por honorarios de la web: su tope anual y los
// archivos (XML y PDF) que se guardan con cada uno en R2. Lee "hoy" de profile.
//
// Es la API pública del módulo: los demás módulos y main solo usan lo de aquí.
package receipts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/storage"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	receiptshttp "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts/adapters/http"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// UploadPath es la ruta de subida (relativa a /api/v1), la única con un cuerpo
// mayor al límite general.
const UploadPath = receiptshttp.UploadPath

// MaxUploadBytes es el cuerpo más grande que acepta esa ruta.
const MaxUploadBytes = domain.MaxUploadBytes

// Module arma el módulo con sus adaptadores.
type Module struct {
	service *app.Service
	handler *receiptshttp.Handler
}

// New crea el módulo.
func New(runner tx.Runner, c clock.Clock, profiles profile.Reader, files storage.Storage) *Module {
	service := app.NewService(app.Deps{
		Receipts:  postgres.NewReceipts(runner),
		Limits:    postgres.NewLimits(runner),
		Documents: postgres.NewDocuments(runner),
		Storage:   fileStorage{files},
		Profile:   profileSource{profiles},
		Tx:        runner,
		Clock:     c,
	})
	return &Module{service: service, handler: receiptshttp.NewHandler(service)}
}

// Routes monta las rutas del módulo; todas piden sesión.
func (m *Module) Routes(protected gin.IRoutes) { m.handler.Routes(protected) }

// OnUserDeleted borra los archivos de la persona (identity.OnUserDeleted).
func (m *Module) OnUserDeleted(ctx context.Context, userID uuid.UUID) error {
	return m.service.OnUserDeleted(ctx, userID)
}

// profileSource adapta profile.Reader al puerto app.Profile.
type profileSource struct{ profiles profile.Reader }

func (p profileSource) Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error) {
	return p.profiles.Today(ctx, userID)
}

// fileStorage adapta storage.Storage al puerto app.FileStorage.
type fileStorage struct{ files storage.Storage }

func (f fileStorage) Put(ctx context.Context, key, contentType string, data []byte) error {
	return translate(f.files.Put(ctx, key, contentType, bytes.NewReader(data), int64(len(data))))
}

func (f fileStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	body, err := f.files.Get(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	return body, translate(err)
}

func (f fileStorage) Delete(ctx context.Context, keys ...string) error {
	return translate(f.files.Delete(ctx, keys...))
}

func (f fileStorage) DeletePrefix(ctx context.Context, prefix string) error {
	return translate(f.files.DeletePrefix(ctx, prefix))
}

func (f fileStorage) DownloadURL(ctx context.Context, key string, ttl time.Duration, fileName, contentType string, download bool) (string, error) {
	url, err := f.files.PresignGet(ctx, key, ttl, storage.GetOptions{FileName: fileName, ContentType: contentType, Download: download})
	return url, translate(err)
}

func translate(err error) error {
	if errors.Is(err, storage.ErrUnavailable) {
		return domain.ErrStorageDisabled
	}
	return err
}
