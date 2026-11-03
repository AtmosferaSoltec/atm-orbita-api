package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/servicetoken"
)

// SystemCode es el sistema dueño de los archivos en atm-storage-api. El nombre
// va en la URL y debe coincidir con el del token de servicio.
const SystemCode = "orbita"

const maxAPIResponseBytes = 1 << 20

// StorageAPI guarda los archivos en atm-storage-api con el token de servicio de
// Orbita. Storage genera el id y la llave del objeto; Orbita solo guarda el id.
type StorageAPI struct {
	baseURL string
	tokens  *servicetoken.Source
	api     *http.Client
	// download baja los bytes de la URL firmada (otro host, sin el token).
	download *http.Client
}

// NewStorageAPI crea el cliente. baseURL es STORAGE_API_URL.
func NewStorageAPI(baseURL string, tokens *servicetoken.Source) *StorageAPI {
	return &StorageAPI{
		baseURL:  strings.TrimRight(baseURL, "/"),
		tokens:   tokens,
		api:      &http.Client{Timeout: 30 * time.Second},
		download: &http.Client{Timeout: 60 * time.Second},
	}
}

// Configured dice si hay URL y credenciales de servicio.
func (s *StorageAPI) Configured() bool { return s.baseURL != "" && s.tokens.Configured() }

// Put sube el archivo como multipart con una sola parte "file". Se arma en
// memoria (los recibos pesan hasta 5 MB) para poder repetir la petición si el
// token venció.
func (s *StorageAPI) Put(ctx context.Context, ownerID uuid.UUID, fileName, contentType string, body io.Reader, _ int64) (string, error) {
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, quoteFileName(fileName)))
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, body); err != nil {
		return "", fmt.Errorf("read file to upload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	query := url.Values{"scope": {"system"}, "purpose": {"file"}, "system": {SystemCode}}
	endpoint := s.baseURL + "/v1/companies/" + ownerID.String() + "/files?" + query.Encode()
	res, err := s.tokens.Do(ctx, s.api, func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(form.Bytes()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req, nil
	})
	if err != nil {
		return "", wrap("upload", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		return "", statusError("upload", res)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxAPIResponseBytes)).Decode(&created); err != nil {
		return "", errors.New("storage-api upload: unreadable response")
	}
	if _, err := uuid.Parse(created.ID); err != nil {
		return "", errors.New("storage-api upload: response without a valid file id")
	}
	return created.ID, nil
}

// Get pide la URL firmada y baja los bytes de ella.
func (s *StorageAPI) Get(ctx context.Context, ref string) (io.ReadCloser, error) {
	signed, err := s.signedURL(ctx, ref)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signed, nil)
	if err != nil {
		return nil, errors.New("storage-api download: invalid signed url")
	}
	res, err := s.download.Do(req)
	if err != nil {
		// El error de net/http lleva la URL firmada: no se propaga.
		return nil, errors.New("storage-api download: signed url request failed")
	}
	switch res.StatusCode {
	case http.StatusOK:
		return res.Body, nil
	case http.StatusNotFound:
		_ = res.Body.Close()
		return nil, ErrNotFound
	}
	_ = res.Body.Close()
	return nil, fmt.Errorf("storage-api download: signed url answered status %d", res.StatusCode)
}

// PresignGet devuelve la URL firmada de storage; ttl y opts no aplican porque
// storage fija el vencimiento (5 minutos) y siempre entrega como descarga.
func (s *StorageAPI) PresignGet(ctx context.Context, ref string, _ time.Duration, _ GetOptions) (string, error) {
	return s.signedURL(ctx, ref)
}

// Delete borra cada archivo; un 404 cuenta como ya borrado. Sigue con los demás
// si uno falla y devuelve todos los errores juntos.
func (s *StorageAPI) Delete(ctx context.Context, refs ...string) error {
	var failures []error
	for _, ref := range refs {
		id, err := uuid.Parse(ref)
		if err != nil {
			continue // no es una referencia de storage: nada que borrar
		}
		endpoint := s.baseURL + "/v1/files/" + id.String()
		res, err := s.tokens.Do(ctx, s.api, func() (*http.Request, error) {
			return http.NewRequest(http.MethodDelete, endpoint, nil)
		})
		if err != nil {
			failures = append(failures, wrap("delete", err))
			continue
		}
		status := res.StatusCode
		failure := statusError("delete", res)
		_ = res.Body.Close()
		if status != http.StatusNoContent && status != http.StatusOK && status != http.StatusNotFound {
			failures = append(failures, failure)
		}
	}
	return errors.Join(failures...)
}

func (s *StorageAPI) signedURL(ctx context.Context, ref string) (string, error) {
	id, err := uuid.Parse(ref)
	if err != nil {
		return "", ErrNotFound
	}
	endpoint := s.baseURL + "/v1/files/" + id.String() + "/download"
	res, err := s.tokens.Do(ctx, s.api, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, endpoint, nil)
	})
	if err != nil {
		return "", wrap("download", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if res.StatusCode != http.StatusOK {
		return "", statusError("download", res)
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxAPIResponseBytes)).Decode(&out); err != nil || out.URL == "" {
		return "", errors.New("storage-api download: response without a url")
	}
	return out.URL, nil
}

// wrap deja pasar la falta de configuración y oculta el detalle de red (la URL
// de storage no es secreta, pero tampoco hace falta en los logs de cada falla).
func wrap(action string, err error) error {
	if errors.Is(err, servicetoken.ErrNotConfigured) {
		return ErrUnavailable
	}
	return fmt.Errorf("storage-api %s: %w", action, err)
}

// statusError describe una respuesta inesperada con su estado y el code de
// storage, nunca el cuerpo completo.
func statusError(action string, res *http.Response) error {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, maxAPIResponseBytes)).Decode(&body)
	if body.Error.Code != "" {
		return fmt.Errorf("storage-api %s: status %d (%s)", action, res.StatusCode, body.Error.Code)
	}
	return fmt.Errorf("storage-api %s: status %d", action, res.StatusCode)
}

// quoteFileName escapa lo que rompería el encabezado del multipart.
func quoteFileName(name string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", "").Replace(name)
}
