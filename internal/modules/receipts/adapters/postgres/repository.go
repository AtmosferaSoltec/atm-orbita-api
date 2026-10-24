// Package postgres implementa los puertos de guardado de receipts con las
// consultas de queries.sql (generadas por sqlc en sqlcgen/).
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

const (
	uniqueViolation = "23505"
	// codeIndex es el índice único de serie + número por persona.
	codeIndex = "receipts_user_code_key"
)

// DB da la conexión de la transacción en curso, o el pool (tx.Runner).
type DB interface {
	DB(ctx context.Context) tx.DBTX
}

func queries(ctx context.Context, db DB) *sqlcgen.Queries { return sqlcgen.New(db.DB(ctx)) }

// Receipts implementa app.Receipts.
type Receipts struct{ db DB }

// NewReceipts crea el repositorio.
func NewReceipts(db DB) Receipts { return Receipts{db: db} }

// Insert guarda el recibo; un id repetido no se pisa.
func (r Receipts) Insert(ctx context.Context, userID uuid.UUID, rc domain.Receipt, at time.Time) (bool, error) {
	n, err := queries(ctx, r.db).InsertReceipt(ctx, sqlcgen.InsertReceiptParams{
		ID: rc.ID, UserID: userID, Series: rc.Series, Number: rc.Number, IssueDate: rc.IssueDate.Time(),
		ClientName: rc.Client.Name, ClientDocType: rc.Client.DocType, ClientDocNumber: rc.Client.DocNumber,
		Description: rc.Description, Gross: rc.Gross.Decimal(), Withholding: rc.Withholding.Decimal(),
		CollectedOn: dateOrNil(rc.CollectedOn), Source: rc.Source, CreatedAt: at,
	})
	if isCodeTaken(err) {
		return false, domain.ErrAlreadyExists
	}
	return n == 1, err
}

// Get busca un recibo de la persona, con sus archivos.
func (r Receipts) Get(ctx context.Context, userID, id uuid.UUID) (domain.Receipt, error) {
	row, err := queries(ctx, r.db).GetReceipt(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return domain.Receipt{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Receipt{}, err
	}
	out, err := r.withDocuments(ctx, userID, []sqlcgen.Receipt{row})
	if err != nil {
		return domain.Receipt{}, err
	}
	return out[0], nil
}

// Owner dice de quién es el id.
func (r Receipts) Owner(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	row, err := queries(ctx, r.db).GetReceipt(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, domain.ErrNotFound
	}
	return row.UserID, err
}

// ListOfYear devuelve los recibos emitidos en el año, con sus archivos.
func (r Receipts) ListOfYear(ctx context.Context, userID uuid.UUID, year int) ([]domain.Receipt, error) {
	rows, err := queries(ctx, r.db).ListReceiptsOfYear(ctx, sqlcgen.ListReceiptsOfYearParams{
		UserID:   userID,
		DateFrom: dates.New(year, time.January, 1).Time(),
		DateTo:   dates.New(year+1, time.January, 1).Time(),
	})
	if err != nil {
		return nil, err
	}
	return r.withDocuments(ctx, userID, rows)
}

// Years son los años con al menos un recibo.
func (r Receipts) Years(ctx context.Context, userID uuid.UUID) ([]int, error) {
	rows, err := queries(ctx, r.db).ListReceiptYears(ctx, userID)
	if err != nil {
		return nil, err
	}
	years := make([]int, 0, len(rows))
	for _, year := range rows {
		years = append(years, int(year))
	}
	return years, nil
}

// Update cambia los datos del recibo propio.
func (r Receipts) Update(ctx context.Context, userID uuid.UUID, rc domain.Receipt) error {
	n, err := queries(ctx, r.db).UpdateReceipt(ctx, sqlcgen.UpdateReceiptParams{
		ID: rc.ID, UserID: userID, Series: rc.Series, Number: rc.Number, IssueDate: rc.IssueDate.Time(),
		ClientName: rc.Client.Name, ClientDocType: rc.Client.DocType, ClientDocNumber: rc.Client.DocNumber,
		Description: rc.Description, Gross: rc.Gross.Decimal(), Withholding: rc.Withholding.Decimal(),
		CollectedOn: dateOrNil(rc.CollectedOn), Source: rc.Source,
	})
	switch {
	case isCodeTaken(err):
		return domain.ErrAlreadyExists
	case err == nil && n == 0:
		return domain.ErrNotFound
	}
	return err
}

// SetCollected fija o quita la fecha de cobro.
func (r Receipts) SetCollected(ctx context.Context, userID, id uuid.UUID, on *dates.LocalDate) error {
	n, err := queries(ctx, r.db).SetReceiptCollected(ctx, sqlcgen.SetReceiptCollectedParams{
		ID: id, UserID: userID, CollectedOn: dateOrNil(on),
	})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// Delete borra el recibo y devuelve las llaves de sus archivos.
func (r Receipts) Delete(ctx context.Context, userID, id uuid.UUID) ([]string, error) {
	q := queries(ctx, r.db)
	keys, err := q.ListStorageKeysOfReceipt(ctx, sqlcgen.ListStorageKeysOfReceiptParams{ReceiptID: id, UserID: userID})
	if err != nil {
		return nil, err
	}
	n, err := q.DeleteReceipt(ctx, sqlcgen.DeleteReceiptParams{ID: id, UserID: userID})
	if err == nil && n == 0 {
		return nil, domain.ErrNotFound
	}
	return keys, err
}

func (r Receipts) withDocuments(ctx context.Context, userID uuid.UUID, rows []sqlcgen.Receipt) ([]domain.Receipt, error) {
	if len(rows) == 0 {
		return []domain.Receipt{}, nil
	}
	receiptIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		receiptIDs = append(receiptIDs, row.ID)
	}
	docs, err := queries(ctx, r.db).ListDocumentsOfReceipts(ctx, sqlcgen.ListDocumentsOfReceiptsParams{
		UserID: userID, ReceiptIds: receiptIDs,
	})
	if err != nil {
		return nil, err
	}
	byReceipt := make(map[uuid.UUID][]domain.Document, len(rows))
	for _, d := range docs {
		byReceipt[d.ReceiptID] = append(byReceipt[d.ReceiptID], toDocument(d))
	}
	out := make([]domain.Receipt, 0, len(rows))
	for _, row := range rows {
		rc := toReceipt(row)
		rc.Documents = byReceipt[row.ID]
		if rc.Documents == nil {
			rc.Documents = []domain.Document{}
		}
		out = append(out, rc)
	}
	return out, nil
}

// Limits implementa app.Limits.
type Limits struct{ db DB }

// NewLimits crea el repositorio.
func NewLimits(db DB) Limits { return Limits{db: db} }

// Get devuelve el tope propio del año, si lo hay.
func (r Limits) Get(ctx context.Context, userID uuid.UUID, year int) (money.Amount, bool, error) {
	amount, err := queries(ctx, r.db).GetYearLimit(ctx, sqlcgen.GetYearLimitParams{UserID: userID, Year: int16(year)}) //nolint:gosec // año validado (2000–2100)
	if errors.Is(err, pgx.ErrNoRows) {
		return money.Amount{}, false, nil
	}
	if err != nil {
		return money.Amount{}, false, err
	}
	return money.NewAmount(amount), true, nil
}

// Set guarda el tope del año.
func (r Limits) Set(ctx context.Context, userID uuid.UUID, year int, amount money.Amount) error {
	return queries(ctx, r.db).UpsertYearLimit(ctx, sqlcgen.UpsertYearLimitParams{
		UserID: userID, Year: int16(year), Amount: amount.Decimal(), //nolint:gosec // año validado (2000–2100)
	})
}

// Documents implementa app.Documents.
type Documents struct{ db DB }

// NewDocuments crea el repositorio.
func NewDocuments(db DB) Documents { return Documents{db: db} }

// Get busca el archivo de un tipo del recibo propio.
func (r Documents) Get(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind) (domain.Document, error) {
	row, err := queries(ctx, r.db).GetDocument(ctx, sqlcgen.GetDocumentParams{ReceiptID: receiptID, UserID: userID, Kind: string(kind)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Document{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Document{}, err
	}
	return toDocument(row), nil
}

// Replace borra la fila anterior del mismo tipo (si la hay) y guarda la nueva.
// Corre en la transacción del caso de uso.
func (r Documents) Replace(ctx context.Context, userID uuid.UUID, doc domain.Document) (string, error) {
	q := queries(ctx, r.db)
	previous := ""
	old, err := q.GetDocument(ctx, sqlcgen.GetDocumentParams{ReceiptID: doc.ReceiptID, UserID: userID, Kind: string(doc.Kind)})
	switch {
	case err == nil:
		previous = old.StorageKey
		if _, err := q.DeleteDocument(ctx, sqlcgen.DeleteDocumentParams{ReceiptID: doc.ReceiptID, UserID: userID, Kind: string(doc.Kind)}); err != nil {
			return "", err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return "", err
	}
	err = q.InsertDocument(ctx, sqlcgen.InsertDocumentParams{
		ID: doc.ID, ReceiptID: doc.ReceiptID, UserID: userID, Kind: string(doc.Kind), FileName: doc.FileName,
		SizeBytes: int32(doc.SizeBytes), ContentType: doc.ContentType, StorageKey: doc.StorageKey, //nolint:gosec // acotado por MaxUploadBytes
		UploadedAt: doc.UploadedAt,
	})
	return previous, err
}

// Delete borra la fila y devuelve la llave del archivo.
func (r Documents) Delete(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind) (string, error) {
	doc, err := r.Get(ctx, userID, receiptID, kind)
	if err != nil {
		return "", err
	}
	if _, err := queries(ctx, r.db).DeleteDocument(ctx, sqlcgen.DeleteDocumentParams{ReceiptID: receiptID, UserID: userID, Kind: string(kind)}); err != nil {
		return "", err
	}
	return doc.StorageKey, nil
}

func toReceipt(row sqlcgen.Receipt) domain.Receipt {
	var collected *dates.LocalDate
	if row.CollectedOn != nil {
		day := dates.FromTime(*row.CollectedOn)
		collected = &day
	}
	return domain.Receipt{
		ID: row.ID, Series: row.Series, Number: row.Number, IssueDate: dates.FromTime(row.IssueDate),
		Client:      domain.Client{Name: row.ClientName, DocType: row.ClientDocType, DocNumber: row.ClientDocNumber},
		Description: row.Description, Gross: money.NewAmount(row.Gross), Withholding: money.NewAmount(row.Withholding),
		CollectedOn: collected, Source: row.Source, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func toDocument(row sqlcgen.ReceiptDocument) domain.Document {
	return domain.Document{
		ID: row.ID, ReceiptID: row.ReceiptID, Kind: domain.DocumentKind(row.Kind), FileName: row.FileName,
		SizeBytes: int(row.SizeBytes), ContentType: row.ContentType, StorageKey: row.StorageKey, UploadedAt: row.UploadedAt,
	}
}

func dateOrNil(day *dates.LocalDate) *time.Time {
	if day == nil {
		return nil
	}
	t := day.Time()
	return &t
}

func isCodeTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == codeIndex
}
