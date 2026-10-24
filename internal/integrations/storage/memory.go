package storage

import (
	"context"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Memory guarda en memoria: para las pruebas.
type Memory struct {
	mu      sync.Mutex
	objects map[string]MemoryObject
}

// MemoryObject es lo guardado en Memory.
type MemoryObject struct {
	ContentType string
	Data        []byte
}

// NewMemory crea un almacenamiento vacío.
func NewMemory() *Memory { return &Memory{objects: map[string]MemoryObject{}} }

// Put guarda el objeto.
func (m *Memory) Put(_ context.Context, key, contentType string, body io.Reader, _ int64) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = MemoryObject{ContentType: contentType, Data: data}
	return nil
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

// DeletePrefix borra por prefijo.
func (m *Memory) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			delete(m.objects, key)
		}
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

// Get devuelve un objeto guardado (para las aserciones de las pruebas).
func (m *Memory) Get(key string) (MemoryObject, bool) {
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
