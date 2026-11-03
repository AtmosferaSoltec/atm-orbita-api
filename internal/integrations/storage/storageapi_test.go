package storage_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/servicetoken"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/storage"
)

// fakeStack simula atm-auth-api, atm-storage-api y el host de las URL firmadas.
type fakeStack struct {
	server *httptest.Server

	mu       sync.Mutex
	files    map[string]string // id -> contenido
	uploads  []string          // path?query de cada subida
	filename string
	fileType string
	bearers  map[string]bool
	deleted  []string
}

func newFakeStack(t *testing.T) *fakeStack {
	t.Helper()
	f := &fakeStack{files: map[string]string{}, bearers: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "service-token", "expires_in": 900})
	})
	mux.HandleFunc("POST /v1/companies/{company}/files", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.bearers[r.Header.Get("Authorization")] = true
		f.uploads = append(f.uploads, r.URL.Path+"?"+r.URL.RawQuery)
		reader, err := r.MultipartReader()
		require.NoError(t, err)
		part, err := reader.NextPart()
		require.NoError(t, err)
		require.Equal(t, "file", part.FormName())
		f.filename, f.fileType = part.FileName(), part.Header.Get("Content-Type")
		data, _ := io.ReadAll(part)
		_, err = reader.NextPart()
		require.ErrorIs(t, err, io.EOF, "exactly one part")
		id := uuid.NewString()
		f.files[id] = string(data)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
	})
	mux.HandleFunc("GET /v1/files/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.bearers[r.Header.Get("Authorization")] = true
		id := r.PathValue("id")
		if _, ok := f.files[id]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"url": f.server.URL + "/signed/" + id + "?sig=abc", "expires_at": "2026-11-02T10:00:00Z"})
	})
	mux.HandleFunc("GET /signed/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusBadRequest) // la URL firmada no lleva el token
			return
		}
		_, _ = io.WriteString(w, f.files[r.PathValue("id")])
	})
	mux.HandleFunc("DELETE /v1/files/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		id := r.PathValue("id")
		f.deleted = append(f.deleted, id)
		if _, ok := f.files[id]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.files, id)
		w.WriteHeader(http.StatusNoContent)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeStack) client() *storage.StorageAPI {
	tokens := servicetoken.New(servicetoken.Config{AuthURL: f.server.URL, ClientID: "orbita", ClientSecret: "s"})
	return storage.NewStorageAPI(f.server.URL+"/", tokens)
}

func TestUploadSendsOneMultipartFilePartAsTheSystem(t *testing.T) {
	f := newFakeStack(t)
	owner := uuid.New()
	ref, err := f.client().Put(context.Background(), owner, "recibo.pdf", "application/pdf", strings.NewReader("%PDF-1.4 hi"), 11)
	require.NoError(t, err)
	_, err = uuid.Parse(ref)
	require.NoError(t, err, "the reference is the storage-api file id")

	assert.Equal(t, []string{"/v1/companies/" + owner.String() + "/files?purpose=file&scope=system&system=orbita"}, f.uploads)
	assert.Equal(t, "recibo.pdf", f.filename)
	assert.Equal(t, "application/pdf", f.fileType)
	assert.Equal(t, "%PDF-1.4 hi", f.files[ref])
	assert.Equal(t, map[string]bool{"Bearer service-token": true}, f.bearers)
}

func TestGetFollowsTheSignedURLWithoutTheToken(t *testing.T) {
	f := newFakeStack(t)
	c := f.client()
	ref, err := c.Put(context.Background(), uuid.New(), "a.xml", "application/xml", strings.NewReader("<x/>"), 4)
	require.NoError(t, err)

	body, err := c.Get(context.Background(), ref)
	require.NoError(t, err)
	data, _ := io.ReadAll(body)
	_ = body.Close()
	assert.Equal(t, "<x/>", string(data))

	signed, err := c.PresignGet(context.Background(), ref, 0, storage.GetOptions{})
	require.NoError(t, err)
	assert.Contains(t, signed, "/signed/"+ref)

	_, err = c.Get(context.Background(), uuid.NewString())
	assert.ErrorIs(t, err, storage.ErrNotFound)
	_, err = c.PresignGet(context.Background(), "not-a-uuid", 0, storage.GetOptions{})
	assert.ErrorIs(t, err, storage.ErrNotFound)
}

func TestDeleteTreatsNotFoundAsDeletedAndKeepsGoing(t *testing.T) {
	f := newFakeStack(t)
	c := f.client()
	ref, err := c.Put(context.Background(), uuid.New(), "a.pdf", "application/pdf", strings.NewReader("%PDF-"), 5)
	require.NoError(t, err)

	missing := uuid.NewString()
	require.NoError(t, c.Delete(context.Background(), missing, ref))
	assert.Equal(t, []string{missing, ref}, f.deleted)
	assert.Empty(t, f.files)
}

func TestDeleteReportsRealFailures(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 900})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"provider_unavailable"}}`))
	}))
	defer broken.Close()
	c := storage.NewStorageAPI(broken.URL, servicetoken.New(servicetoken.Config{AuthURL: broken.URL, ClientID: "orbita", ClientSecret: "s"}))
	err := c.Delete(context.Background(), uuid.NewString())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestWithoutCredentialsEverythingIsUnavailable(t *testing.T) {
	c := storage.NewStorageAPI("http://storage", servicetoken.New(servicetoken.Config{AuthURL: "http://auth", ClientID: "orbita"}))
	assert.False(t, c.Configured())
	_, err := c.Put(context.Background(), uuid.New(), "a", "", strings.NewReader("x"), 1)
	assert.ErrorIs(t, err, storage.ErrUnavailable)
	assert.ErrorIs(t, c.Delete(context.Background(), uuid.NewString()), storage.ErrUnavailable)
}
