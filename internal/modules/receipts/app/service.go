// Package app son los casos de uso de los recibos por honorarios y los puertos
// que necesitan. No conoce Gin, Postgres ni R2.
package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// DownloadURLTTL es cuánto dura una URL firmada de un archivo.
const DownloadURLTTL = 5 * time.Minute

// Receipts guarda los recibos.
type Receipts interface {
	// Insert devuelve false si el id ya existía (no lo pisa) y
	// domain.ErrAlreadyExists si la serie y el número ya están registrados.
	Insert(ctx context.Context, userID uuid.UUID, r domain.Receipt, at time.Time) (bool, error)
	// Get devuelve domain.ErrNotFound si no existe o es de otra persona.
	Get(ctx context.Context, userID, id uuid.UUID) (domain.Receipt, error)
	// Owner dice de quién es un id, para distinguir "ya es tuyo" de "lo usa otro".
	Owner(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
	ListOfYear(ctx context.Context, userID uuid.UUID, year int) ([]domain.Receipt, error)
	Years(ctx context.Context, userID uuid.UUID) ([]int, error)
	Update(ctx context.Context, userID uuid.UUID, r domain.Receipt) error
	SetCollected(ctx context.Context, userID, id uuid.UUID, on *dates.LocalDate) error
	// Delete borra el recibo (sus documentos se van por cascada) y devuelve las
	// llaves de R2 que tenía.
	Delete(ctx context.Context, userID, id uuid.UUID) ([]string, error)
}

// Limits guarda el tope de cada año.
type Limits interface {
	// Get devuelve ok=false si el año no tiene tope propio.
	Get(ctx context.Context, userID uuid.UUID, year int) (money.Amount, bool, error)
	Set(ctx context.Context, userID uuid.UUID, year int, amount money.Amount) error
}

// Documents guarda los datos de los archivos (los bytes van a FileStorage).
type Documents interface {
	// Get devuelve domain.ErrNotFound si el recibo no tiene ese tipo de archivo.
	Get(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind) (domain.Document, error)
	// Replace guarda doc en lugar del anterior del mismo tipo y devuelve la llave
	// del anterior ("" si no había).
	Replace(ctx context.Context, userID uuid.UUID, doc domain.Document) (string, error)
	// Delete devuelve la llave del archivo borrado.
	Delete(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind) (string, error)
}

// FileStorage es el almacenamiento de archivos (R2). Devuelve
// domain.ErrStorageDisabled si no está configurado.
type FileStorage interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	// Open devuelve domain.ErrNotFound si el objeto ya no está.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, keys ...string) error
	DeletePrefix(ctx context.Context, prefix string) error
	DownloadURL(ctx context.Context, key string, ttl time.Duration, fileName, contentType string, download bool) (string, error)
}

// Profile da "hoy" en la zona horaria de la persona.
type Profile interface {
	Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error)
}

// Transactor abre una transacción (platform/tx.Runner).
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Deps son los puertos del servicio.
type Deps struct {
	Receipts  Receipts
	Limits    Limits
	Documents Documents
	Storage   FileStorage
	Profile   Profile
	Tx        Transactor
	Clock     clock.Clock
}

// Service son los casos de uso.
type Service struct{ deps Deps }

// NewService crea el servicio.
func NewService(deps Deps) *Service { return &Service{deps: deps} }

// ReceiptInput son los datos de crear o editar un recibo.
type ReceiptInput struct {
	Series      string
	Number      string
	IssueDate   dates.LocalDate
	Client      domain.Client
	Description string
	Gross       money.Amount
	Withholding money.Amount
	CollectedOn *dates.LocalDate
	Source      string
}

func (in ReceiptInput) receipt(id uuid.UUID) domain.Receipt {
	r := domain.Receipt{
		ID: id, Series: in.Series, Number: in.Number, IssueDate: in.IssueDate, Client: in.Client,
		Description: in.Description, Gross: in.Gross, Withholding: in.Withholding,
		CollectedOn: in.CollectedOn, Source: in.Source,
	}
	r.Normalize()
	return r
}

// List devuelve los recibos de un año, del más reciente al más antiguo.
func (s *Service) List(ctx context.Context, userID uuid.UUID, year int) ([]domain.Receipt, error) {
	if !domain.ValidYear(year) {
		return nil, yearError()
	}
	return s.deps.Receipts.ListOfYear(ctx, userID, year)
}

// Years son los años con recibos más el año actual, del más reciente al más antiguo.
func (s *Service) Years(ctx context.Context, userID uuid.UUID) (years []int, current int, err error) {
	today, err := s.deps.Profile.Today(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	current = today.Time().Year()
	stored, err := s.deps.Receipts.Years(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	years = append([]int{current}, stored...)
	slices.Sort(years)
	years = slices.Compact(years)
	slices.Reverse(years)
	return years, current, nil
}

// Get devuelve un recibo.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (domain.Receipt, error) {
	return s.deps.Receipts.Get(ctx, userID, id)
}

// Create registra un recibo. Con el id de la app, reintentar no duplica:
// devuelve created=false y el recibo que ya estaba.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, id *uuid.UUID, in ReceiptInput) (domain.Receipt, bool, error) {
	receiptID := ids.New()
	if id != nil {
		receiptID = *id
	}
	r := in.receipt(receiptID)
	today, err := s.deps.Profile.Today(ctx, userID)
	if err != nil {
		return domain.Receipt{}, false, err
	}
	if err := r.Validate(today); err != nil {
		return domain.Receipt{}, false, err
	}
	inserted, err := s.deps.Receipts.Insert(ctx, userID, r, s.deps.Clock.Now())
	if err != nil {
		return domain.Receipt{}, false, err
	}
	if !inserted {
		owner, err := s.deps.Receipts.Owner(ctx, receiptID)
		if err != nil {
			return domain.Receipt{}, false, err
		}
		if owner != userID {
			return domain.Receipt{}, false, domain.ErrIDTaken
		}
	}
	stored, err := s.deps.Receipts.Get(ctx, userID, receiptID)
	return stored, inserted, err
}

// Update cambia los datos de un recibo. Sus archivos se quedan.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in ReceiptInput) (domain.Receipt, error) {
	r := in.receipt(id)
	today, err := s.deps.Profile.Today(ctx, userID)
	if err != nil {
		return domain.Receipt{}, err
	}
	if err := r.Validate(today); err != nil {
		return domain.Receipt{}, err
	}
	if err := s.deps.Receipts.Update(ctx, userID, r); err != nil {
		return domain.Receipt{}, err
	}
	return s.deps.Receipts.Get(ctx, userID, id)
}

// SetCollected marca (o desmarca, con nil) el día en que el cliente pagó.
func (s *Service) SetCollected(ctx context.Context, userID, id uuid.UUID, on *dates.LocalDate) (domain.Receipt, error) {
	current, err := s.deps.Receipts.Get(ctx, userID, id)
	if err != nil {
		return domain.Receipt{}, err
	}
	current.CollectedOn = on
	today, err := s.deps.Profile.Today(ctx, userID)
	if err != nil {
		return domain.Receipt{}, err
	}
	if err := current.Validate(today); err != nil {
		return domain.Receipt{}, err
	}
	if err := s.deps.Receipts.SetCollected(ctx, userID, id, on); err != nil {
		return domain.Receipt{}, err
	}
	return s.deps.Receipts.Get(ctx, userID, id)
}

// Delete borra el recibo y, ya confirmado, sus archivos de R2.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	keys, err := s.deps.Receipts.Delete(ctx, userID, id)
	if err != nil {
		return err
	}
	s.deleteFiles(ctx, keys...)
	return nil
}

// Limit es el tope de un año.
type Limit struct {
	Year      int
	Amount    money.Amount
	IsDefault bool
}

// GetLimit devuelve el tope del año, o el por defecto.
func (s *Service) GetLimit(ctx context.Context, userID uuid.UUID, year int) (Limit, error) {
	if !domain.ValidYear(year) {
		return Limit{}, yearError()
	}
	amount, ok, err := s.deps.Limits.Get(ctx, userID, year)
	if err != nil {
		return Limit{}, err
	}
	if !ok {
		return Limit{Year: year, Amount: domain.DefaultYearlyLimit, IsDefault: true}, nil
	}
	return Limit{Year: year, Amount: amount}, nil
}

// SetLimit fija el tope de un año.
func (s *Service) SetLimit(ctx context.Context, userID uuid.UUID, year int, amount money.Amount) (Limit, error) {
	var problems domain.ValidationError
	if !domain.ValidYear(year) {
		problems.Add("year", domain.CodeYearInvalid)
	}
	if !amount.IsPositive() {
		problems.Add("amount", domain.CodeAmountInvalid)
	}
	if err := problems.OrNil(); err != nil {
		return Limit{}, err
	}
	if err := s.deps.Limits.Set(ctx, userID, year, amount); err != nil {
		return Limit{}, err
	}
	return Limit{Year: year, Amount: amount}, nil
}

// Upload guarda un archivo del recibo, en lugar del anterior del mismo tipo.
// Primero sube a R2 y luego guarda la fila: si la base falla, el objeto nuevo
// se borra; si todo sale bien, se borra el anterior.
func (s *Service) Upload(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind, fileName string, data []byte) (domain.Document, error) {
	receipt, err := s.deps.Receipts.Get(ctx, userID, receiptID)
	if err != nil {
		return domain.Document{}, err
	}
	if err := domain.CheckFile(kind, data, receipt); err != nil {
		return domain.Document{}, err
	}
	doc := domain.Document{
		ID: ids.New(), ReceiptID: receiptID, Kind: kind,
		FileName: domain.CleanFileName(fileName, kind), SizeBytes: len(data),
		ContentType: kind.ContentType(), UploadedAt: s.deps.Clock.Now(),
	}
	doc.StorageKey = domain.StorageKey(userID, receiptID, doc.ID, kind)
	if err := s.deps.Storage.Put(ctx, doc.StorageKey, doc.ContentType, bytes.Clone(data)); err != nil {
		return domain.Document{}, err
	}
	var previous string
	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		previous, err = s.deps.Documents.Replace(ctx, userID, doc)
		return err
	})
	if err != nil {
		s.deleteFiles(ctx, doc.StorageKey)
		return domain.Document{}, err
	}
	if previous != "" {
		s.deleteFiles(ctx, previous)
	}
	return doc, nil
}

// DownloadURL firma una URL corta para bajar el archivo directo de R2.
func (s *Service) DownloadURL(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind, download bool) (string, error) {
	doc, err := s.deps.Documents.Get(ctx, userID, receiptID, kind)
	if err != nil {
		return "", err
	}
	return s.deps.Storage.DownloadURL(ctx, doc.StorageKey, DownloadURLTTL, doc.FileName, doc.ContentType, download)
}

// Content abre el archivo para mostrarlo dentro de la web (el XML formateado, el
// PDF embebido). Leerlo de la URL firmada desde el navegador pediría CORS en el
// bucket, que la llave de R2 no puede configurar (ver plan 18, desviaciones).
func (s *Service) Content(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind) (domain.Document, io.ReadCloser, error) {
	doc, err := s.deps.Documents.Get(ctx, userID, receiptID, kind)
	if err != nil {
		return domain.Document{}, nil, err
	}
	body, err := s.deps.Storage.Open(ctx, doc.StorageKey)
	if err != nil {
		return domain.Document{}, nil, err
	}
	return doc, body, nil
}

// DeleteDocument quita un archivo; los datos del recibo no cambian.
func (s *Service) DeleteDocument(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind) error {
	key, err := s.deps.Documents.Delete(ctx, userID, receiptID, kind)
	if err != nil {
		return err
	}
	s.deleteFiles(ctx, key)
	return nil
}

// OnUserDeleted borra de R2 todos los archivos de la persona. Corre dentro de la
// transacción de eliminar la cuenta: si R2 falla, la cuenta no se borra (así no
// quedan archivos de alguien que ya no existe). Sin R2 configurado no hay nada
// que borrar.
func (s *Service) OnUserDeleted(ctx context.Context, userID uuid.UUID) error {
	err := s.deps.Storage.DeletePrefix(ctx, domain.UserPrefix(userID))
	if errors.Is(err, domain.ErrStorageDisabled) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete receipt files: %w", err)
	}
	return nil
}

// deleteFiles borra objetos ya sin fila. Si R2 falla queda un objeto huérfano,
// nunca un dato a medias: se registra y se sigue.
func (s *Service) deleteFiles(ctx context.Context, keys ...string) {
	if len(keys) == 0 {
		return
	}
	if err := s.deps.Storage.Delete(ctx, keys...); err != nil && !errors.Is(err, domain.ErrStorageDisabled) {
		logger.From(ctx).Warn("receipts: orphan files left in storage", "keys", keys, "error", err)
	}
}

func yearError() error {
	return &domain.ValidationError{Fields: []domain.FieldError{{Field: "year", Code: domain.CodeYearInvalid}}}
}
