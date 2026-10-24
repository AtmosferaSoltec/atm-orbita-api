// Package http traduce HTTP ⇄ casos de uso de los recibos por honorarios.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Service son los casos de uso que usa el handler.
type Service interface {
	List(ctx context.Context, userID uuid.UUID, year int) ([]domain.Receipt, error)
	Years(ctx context.Context, userID uuid.UUID) ([]int, int, error)
	Get(ctx context.Context, userID, id uuid.UUID) (domain.Receipt, error)
	Create(ctx context.Context, userID uuid.UUID, id *uuid.UUID, in app.ReceiptInput) (domain.Receipt, bool, error)
	Update(ctx context.Context, userID, id uuid.UUID, in app.ReceiptInput) (domain.Receipt, error)
	SetCollected(ctx context.Context, userID, id uuid.UUID, on *dates.LocalDate) (domain.Receipt, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
	GetLimit(ctx context.Context, userID uuid.UUID, year int) (app.Limit, error)
	SetLimit(ctx context.Context, userID uuid.UUID, year int, amount money.Amount) (app.Limit, error)
	Upload(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind, fileName string, data []byte) (domain.Document, error)
	DownloadURL(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind, download bool) (string, error)
	DeleteDocument(ctx context.Context, userID, receiptID uuid.UUID, kind domain.DocumentKind) error
}

var (
	errAlreadyExists = apierror.New(http.StatusConflict, "RECEIPT_ALREADY_EXISTS",
		"Ya registraste un recibo con esa serie y número.")
	errXMLMismatch = apierror.New(http.StatusUnprocessableEntity, "XML_RECEIPT_MISMATCH",
		"Este XML es de otro recibo.")
	errFileInvalid = apierror.New(http.StatusBadRequest, "FILE_INVALID",
		"El archivo no es válido para este recibo.")
	errStorageUnavailable = apierror.New(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE",
		"No se pueden guardar archivos en este momento. Inténtalo más tarde.")
)

// UploadPath es la ruta de subida de archivos: la única con un cuerpo mayor al
// límite general (main se la indica al middleware BodyLimit).
const UploadPath = "/receipts/:id/documents/:kind"

// Handler atiende la sección Recibos de la web.
type Handler struct {
	service Service
}

// NewHandler crea el handler.
func NewHandler(service Service) *Handler { return &Handler{service: service} }

// Routes monta las rutas, todas privadas.
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/receipts", h.list)
	protected.GET("/receipts/years", h.years)
	protected.POST("/receipts", h.create)
	protected.GET("/receipts/:id", h.get)
	protected.PATCH("/receipts/:id", h.update)
	protected.PUT("/receipts/:id/collected", h.setCollected)
	protected.DELETE("/receipts/:id", h.delete)
	protected.PUT(UploadPath, h.upload)
	protected.GET("/receipts/:id/documents/:kind/url", h.downloadURL)
	protected.DELETE("/receipts/:id/documents/:kind", h.deleteDocument)
	protected.GET("/receipt-limits/:year", h.getLimit)
	protected.PUT("/receipt-limits/:year", h.setLimit)
}

type clientBody struct {
	Name      string `json:"name"`
	DocType   string `json:"doc_type"`
	DocNumber string `json:"doc_number"`
}

type receiptRequest struct {
	ID          *string    `json:"id"`
	Series      string     `json:"series"`
	Number      string     `json:"number"`
	IssueDate   string     `json:"issue_date"`
	Client      clientBody `json:"client"`
	Description string     `json:"description"`
	Gross       string     `json:"gross"`
	Withholding string     `json:"withholding"`
	CollectedOn *string    `json:"collected_on"`
	Source      string     `json:"source"`
}

type documentResponse struct {
	ID         uuid.UUID `json:"id"`
	Kind       string    `json:"kind"`
	FileName   string    `json:"file_name"`
	SizeBytes  int       `json:"size_bytes"`
	UploadedAt time.Time `json:"uploaded_at"`
}

type receiptResponse struct {
	ID          uuid.UUID          `json:"id"`
	Series      string             `json:"series"`
	Number      string             `json:"number"`
	IssueDate   dates.LocalDate    `json:"issue_date"`
	Client      clientBody         `json:"client"`
	Description string             `json:"description"`
	Gross       money.Amount       `json:"gross"`
	Withholding money.Amount       `json:"withholding"`
	Net         money.Amount       `json:"net"`
	CollectedOn *dates.LocalDate   `json:"collected_on"`
	Source      string             `json:"source"`
	Documents   []documentResponse `json:"documents"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

type limitResponse struct {
	Year      int          `json:"year"`
	Amount    money.Amount `json:"amount"`
	IsDefault bool         `json:"is_default"`
}

func (h *Handler) list(c *gin.Context) {
	year, ok := intParam(c, c.Query("year"), "year")
	if !ok {
		return
	}
	receipts, err := h.service.List(c.Request.Context(), userID(c), year)
	if err != nil {
		respondError(c, err)
		return
	}
	out := make([]receiptResponse, 0, len(receipts))
	for _, r := range receipts {
		out = append(out, toReceiptResponse(r))
	}
	apierror.OK(c, out)
}

func (h *Handler) years(c *gin.Context) {
	years, current, err := h.service.Years(c.Request.Context(), userID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, gin.H{"years": years, "current_year": current})
}

func (h *Handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	receipt, err := h.service.Get(c.Request.Context(), userID(c), id)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toReceiptResponse(receipt))
}

func (h *Handler) create(c *gin.Context) {
	var req receiptRequest
	if !bind(c, &req) {
		return
	}
	var id *uuid.UUID
	if req.ID != nil {
		parsed, err := uuid.Parse(*req.ID)
		if err != nil {
			apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "id", Code: domain.CodeInvalidFormat}))
			return
		}
		id = &parsed
	}
	in, ok := toInput(c, req)
	if !ok {
		return
	}
	receipt, created, err := h.service.Create(c.Request.Context(), userID(c), id, in)
	if err != nil {
		respondError(c, err)
		return
	}
	if created {
		apierror.Created(c, toReceiptResponse(receipt))
		return
	}
	apierror.OK(c, toReceiptResponse(receipt))
}

func (h *Handler) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req receiptRequest
	if !bind(c, &req) {
		return
	}
	in, ok := toInput(c, req)
	if !ok {
		return
	}
	receipt, err := h.service.Update(c.Request.Context(), userID(c), id, in)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toReceiptResponse(receipt))
}

func (h *Handler) setCollected(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		CollectedOn *string `json:"collected_on"`
	}
	if !bind(c, &req) {
		return
	}
	on, ok := optionalDate(c, req.CollectedOn, "collected_on")
	if !ok {
		return
	}
	receipt, err := h.service.SetCollected(c.Request.Context(), userID(c), id, on)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toReceiptResponse(receipt))
}

func (h *Handler) delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), userID(c), id); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

func (h *Handler) getLimit(c *gin.Context) {
	year, ok := intParam(c, c.Param("year"), "year")
	if !ok {
		return
	}
	limit, err := h.service.GetLimit(c.Request.Context(), userID(c), year)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, limitResponse{Year: limit.Year, Amount: limit.Amount, IsDefault: limit.IsDefault})
}

func (h *Handler) setLimit(c *gin.Context) {
	year, ok := intParam(c, c.Param("year"), "year")
	if !ok {
		return
	}
	var req struct {
		Amount string `json:"amount"`
	}
	if !bind(c, &req) {
		return
	}
	amount, err := money.ParseAmount(req.Amount)
	if err != nil {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "amount", Code: domain.CodeAmountInvalid}))
		return
	}
	limit, err := h.service.SetLimit(c.Request.Context(), userID(c), year, amount)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, limitResponse{Year: limit.Year, Amount: limit.Amount, IsDefault: false})
}

// upload recibe multipart/form-data con el campo "file".
func (h *Handler) upload(c *gin.Context) {
	id, kind, ok := documentParams(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, domain.MaxUploadBytes)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierror.Respond(c, err)
			return
		}
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "file", Code: domain.CodeInvalidFormat}))
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, int64(kind.MaxBytes())+1))
	if err != nil {
		apierror.Respond(c, err)
		return
	}
	if len(data) > kind.MaxBytes() {
		apierror.Respond(c, errFileInvalid)
		return
	}
	doc, err := h.service.Upload(c.Request.Context(), userID(c), id, kind, header.Filename, data)
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, toDocumentResponse(doc))
}

func (h *Handler) downloadURL(c *gin.Context) {
	id, kind, ok := documentParams(c)
	if !ok {
		return
	}
	url, err := h.service.DownloadURL(c.Request.Context(), userID(c), id, kind, c.Query("download") == "1")
	if err != nil {
		respondError(c, err)
		return
	}
	apierror.OK(c, gin.H{"url": url, "expires_in": int(app.DownloadURLTTL.Seconds())})
}

func (h *Handler) deleteDocument(c *gin.Context) {
	id, kind, ok := documentParams(c)
	if !ok {
		return
	}
	if err := h.service.DeleteDocument(c.Request.Context(), userID(c), id, kind); err != nil {
		respondError(c, err)
		return
	}
	apierror.NoContent(c)
}

// toInput lee montos y fechas del cuerpo; uno mal formado es un error de su campo.
func toInput(c *gin.Context, req receiptRequest) (app.ReceiptInput, bool) {
	var problems []apierror.FieldError
	issueDate, err := dates.Parse(req.IssueDate)
	if err != nil && req.IssueDate != "" {
		problems = append(problems, apierror.FieldError{Field: "issue_date", Code: domain.CodeInvalidFormat})
	}
	gross, err := money.ParseAmount(req.Gross)
	if err != nil {
		problems = append(problems, apierror.FieldError{Field: "gross", Code: domain.CodeAmountInvalid})
	}
	withholding := money.Amount{}
	if req.Withholding != "" {
		if withholding, err = money.ParseAmount(req.Withholding); err != nil {
			problems = append(problems, apierror.FieldError{Field: "withholding", Code: domain.CodeWithholdingInvalid})
		}
	}
	var collected *dates.LocalDate
	if req.CollectedOn != nil && *req.CollectedOn != "" {
		day, err := dates.Parse(*req.CollectedOn)
		if err != nil {
			problems = append(problems, apierror.FieldError{Field: "collected_on", Code: domain.CodeInvalidFormat})
		}
		collected = &day
	}
	if len(problems) > 0 {
		apierror.Respond(c, apierror.Validation(problems...))
		return app.ReceiptInput{}, false
	}
	return app.ReceiptInput{
		Series: req.Series, Number: req.Number, IssueDate: issueDate,
		Client:      domain.Client{Name: req.Client.Name, DocType: req.Client.DocType, DocNumber: req.Client.DocNumber},
		Description: req.Description, Gross: gross, Withholding: withholding, CollectedOn: collected, Source: req.Source,
	}, true
}

func toReceiptResponse(r domain.Receipt) receiptResponse {
	docs := make([]documentResponse, 0, len(r.Documents))
	for _, d := range r.Documents {
		docs = append(docs, toDocumentResponse(d))
	}
	return receiptResponse{
		ID: r.ID, Series: r.Series, Number: r.Number, IssueDate: r.IssueDate,
		Client:      clientBody{Name: r.Client.Name, DocType: r.Client.DocType, DocNumber: r.Client.DocNumber},
		Description: r.Description, Gross: r.Gross, Withholding: r.Withholding, Net: r.Net(),
		CollectedOn: r.CollectedOn, Source: r.Source, Documents: docs,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func toDocumentResponse(d domain.Document) documentResponse {
	return documentResponse{ID: d.ID, Kind: string(d.Kind), FileName: d.FileName, SizeBytes: d.SizeBytes, UploadedAt: d.UploadedAt.UTC()}
}

func userID(c *gin.Context) uuid.UUID {
	principal, _ := auth.FromContext(c.Request.Context())
	return principal.UserID
}

// pathID lee el id de la ruta. Uno que no es UUID no puede existir: 404.
func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.UUID{}, false
	}
	return id, true
}

func documentParams(c *gin.Context) (uuid.UUID, domain.DocumentKind, bool) {
	id, ok := pathID(c)
	if !ok {
		return uuid.UUID{}, "", false
	}
	kind, ok := domain.ParseKind(c.Param("kind"))
	if !ok {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.UUID{}, "", false
	}
	return id, kind, true
}

func intParam(c *gin.Context, text, field string) (int, bool) {
	value, err := strconv.Atoi(text)
	if err != nil || !domain.ValidYear(value) {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: field, Code: domain.CodeYearInvalid}))
		return 0, false
	}
	return value, true
}

func optionalDate(c *gin.Context, text *string, field string) (*dates.LocalDate, bool) {
	if text == nil || *text == "" {
		return nil, true
	}
	day, err := dates.Parse(*text)
	if err != nil {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: field, Code: domain.CodeInvalidFormat}))
		return nil, false
	}
	return &day, true
}

func bind(c *gin.Context, target any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierror.Respond(c, err)
			return false
		}
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "body", Code: domain.CodeInvalidFormat}))
		return false
	}
	return true
}

func respondError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		fields := make([]apierror.FieldError, 0, len(validation.Fields))
		for _, f := range validation.Fields {
			fields = append(fields, apierror.FieldError{Field: f.Field, Code: f.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.Is(err, domain.ErrNotFound):
		apierror.Respond(c, apierror.ErrNotFound)
	case errors.Is(err, domain.ErrIDTaken):
		apierror.Respond(c, apierror.ErrIDConflict)
	case errors.Is(err, domain.ErrAlreadyExists):
		apierror.Respond(c, errAlreadyExists)
	case errors.Is(err, domain.ErrXMLMismatch):
		apierror.Respond(c, errXMLMismatch)
	case errors.Is(err, domain.ErrFileInvalid):
		apierror.Respond(c, errFileInvalid)
	case errors.Is(err, domain.ErrStorageDisabled):
		apierror.Respond(c, errStorageUnavailable)
	default:
		apierror.Respond(c, err)
	}
}
