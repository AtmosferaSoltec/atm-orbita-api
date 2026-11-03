package storage

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
)

// Memory guarda en memoria: para las pruebas.
type Memory struct {
	mu      sync.Mutex
	objects map[string]MemoryObject
}

// MemoryObject es lo guardado en Memory.
type MemoryObject struct {
	OwnerID     uuid.UUID
	FileName    string
	ContentType string
	Data        []byte
}

// NewMemory crea un almacenamiento vacío.
func NewMemory() *Memory { return &Memory{objects: map[string]MemoryObject{}} }

// Put guarda el objeto.
func (m *Memory) Put(_ context.Context, ownerID uuid.UUID, fileName, contentType string, body io.Reader, _ int64) (string, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	// La referencia es un UUID, como la de atm-storage-api.
	key := ids.New().String()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = MemoryObject{OwnerID: ownerID, FileName: fileName, ContentType: contentType, Data: data}
	return key, nil
}

// Get devuelve el objeto.
func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	object, ok := m.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(object.Data)), nil
}

// Delete borra las llaves.
func (m *Memory) Delete(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range keys {
		delete(m.objects, key)
	}
	return nil
}

// PresignGet da una URL ficticia con la llave.
func (m *Memory) PresignGet(_ context.Context, key string, _ time.Duration, opts GetOptions) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[key]; !ok {
		return "", ErrNotFound
	}
	query := url.Values{}
	if opts.Download {
		query.Set("download", "1")
	}
	return "memory://" + key + "?" + query.Encode(), nil
}

// Object devuelve un objeto guardado (para las aserciones de las pruebas).
func (m *Memory) Object(key string) (MemoryObject, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	object, ok := m.objects[key]
	return object, ok
}

// Keys devuelve las llaves guardadas, ordenadas.
func (m *Memory) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.objects))
	for key := range m.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
