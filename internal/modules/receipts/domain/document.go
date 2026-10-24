package domain

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DocumentKind es el tipo de archivo de un recibo: su XML o su PDF.
type DocumentKind string

// Tipos de archivo.
const (
	KindXML DocumentKind = "xml"
	KindPDF DocumentKind = "pdf"
)

// Tamaños máximos por tipo.
const (
	MaxXMLBytes = 1_000_000
	MaxPDFBytes = 5_000_000
	// MaxUploadBytes es el cuerpo más grande que acepta la ruta de subida (PDF + multipart).
	MaxUploadBytes = 6 << 20
	fileNameMax    = 255
)

// Document es un archivo guardado con un recibo. Los bytes viven en R2.
type Document struct {
	ID          uuid.UUID
	ReceiptID   uuid.UUID
	Kind        DocumentKind
	FileName    string
	SizeBytes   int
	ContentType string
	StorageKey  string
	UploadedAt  time.Time
}

// ParseKind lee el tipo de la ruta.
func ParseKind(text string) (DocumentKind, bool) {
	switch DocumentKind(text) {
	case KindXML:
		return KindXML, true
	case KindPDF:
		return KindPDF, true
	}
	return "", false
}

// ContentType es el que se guarda en R2 y con el que se entrega.
func (k DocumentKind) ContentType() string {
	if k == KindPDF {
		return "application/pdf"
	}
	return "application/xml"
}

// MaxBytes es el tamaño máximo del tipo.
func (k DocumentKind) MaxBytes() int {
	if k == KindPDF {
		return MaxPDFBytes
	}
	return MaxXMLBytes
}

// StorageKey es la llave en R2. El usuario va delante para poder borrar todo lo
// suyo por prefijo al eliminar la cuenta.
func StorageKey(userID, receiptID, documentID uuid.UUID, kind DocumentKind) string {
	return fmt.Sprintf("%s%s/%s.%s", UserPrefix(userID), receiptID, documentID, kind)
}

// UserPrefix es donde viven todos los archivos de recibos de una persona.
func UserPrefix(userID uuid.UUID) string { return fmt.Sprintf("receipts/%s/", userID) }

// CleanFileName deja solo el nombre (sin carpetas) y lo acota; vacío usa uno por defecto.
func CleanFileName(name string, kind DocumentKind) string {
	base := strings.TrimSpace(path.Base(strings.ReplaceAll(name, "\\", "/")))
	if base == "" || base == "." || base == "/" {
		base = "recibo." + string(kind)
	}
	if len(base) > fileNameMax {
		base = base[len(base)-fileNameMax:]
	}
	return base
}

// CheckFile revisa el contenido: un PDF real empieza con %PDF-; un XML debe ser
// el del recibo (su cbc:ID). Devuelve ErrFileInvalid o ErrXMLMismatch.
func CheckFile(kind DocumentKind, data []byte, receipt Receipt) error {
	if len(data) == 0 || len(data) > kind.MaxBytes() {
		return ErrFileInvalid
	}
	if kind == KindPDF {
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			return ErrFileInvalid
		}
		return nil
	}
	code, err := XMLReceiptCode(data)
	if err != nil {
		return ErrFileInvalid
	}
	series, number, _ := strings.Cut(code, "-")
	if Code(series, number) != receipt.Code() {
		return ErrXMLMismatch
	}
	return nil
}

// XMLReceiptCode lee el cbc:ID que es hijo directo de la raíz del XML de SUNAT
// ("E001-109"). Hay otros ID anidados (OrderReference, firma): esos no cuentan.
func XMLReceiptCode(data []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	inID := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return "", errors.New("xml without receipt id")
		}
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			inID = depth == 2 && t.Name.Local == "ID"
		case xml.CharData:
			if inID {
				if code := strings.TrimSpace(string(t)); code != "" {
					return code, nil
				}
			}
		case xml.EndElement:
			depth--
			inID = false
		}
	}
}
