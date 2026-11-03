// Package servicetoken obtiene y cachea el token de servicio del sistema orbita en
// atm-auth-api (POST /v1/token). Con él Orbita llama a atm-storage-api y a
// atm-documents-api: no actúa en nombre de una persona sino como sistema, así que
// el mismo token sirve a todas las peticiones hasta que vence.
package servicetoken

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrNotConfigured: falta AUTH_URL o AUTH_CLIENT_SECRET; los servicios que
// dependen del token quedan apagados en vez de impedir el arranque.
var ErrNotConfigured = errors.New("service token is not configured")

// refreshMargin es cuánto antes de vencer se pide uno nuevo, para que ninguna
// petición salga con un token a punto de caducar.
const refreshMargin = 60 * time.Second

// maxResponseBytes acota la respuesta de auth.
const maxResponseBytes = 64 << 10

// Config son los datos del cliente de servicio.
type Config struct {
	AuthURL      string
	ClientID     string
	ClientSecret string
	// HTTPClient y Now existen para las pruebas.
	HTTPClient *http.Client
	Now        func() time.Time
}

// Source entrega el token vigente.
type Source struct {
	cfg Config

	// mu se mantiene durante la petición a auth: varias llamadas simultáneas con el
	// token vencido esperan a la primera y reutilizan su resultado (single-flight).
	mu      sync.Mutex
	token   string
	refresh time.Time
}

// New crea la fuente. Nunca falla: sin configuración, Token devuelve ErrNotConfigured.
func New(cfg Config) *Source {
	cfg.AuthURL = strings.TrimRight(cfg.AuthURL, "/")
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Source{cfg: cfg}
}

// Configured dice si hay con qué pedir tokens.
func (s *Source) Configured() bool {
	return s != nil && s.cfg.AuthURL != "" && s.cfg.ClientID != "" && s.cfg.ClientSecret != ""
}

// Token devuelve el token vigente, pidiendo uno nuevo si falta o está por vencer.
func (s *Source) Token(ctx context.Context) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.cfg.Now().Before(s.refresh) {
		return s.token, nil
	}
	token, lifetime, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	margin := refreshMargin
	if lifetime <= 2*margin {
		margin = lifetime / 2
	}
	s.token = token
	s.refresh = s.cfg.Now().Add(lifetime - margin)
	return token, nil
}

// Invalidate descarta el token si sigue siendo el que falló (un 401 aguas abajo:
// auth pudo rotar su llave o revocarlo). Si otro ya lo renovó, no toca el nuevo.
func (s *Source) Invalidate(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == token {
		s.token = ""
	}
}

// Do envía la petición que arma build con el token de servicio. Si el servicio
// responde 401 renueva el token y la repite una sola vez; por eso build crea una
// petición nueva en cada intento (el cuerpo no se puede releer). Quien llama
// cierra el cuerpo de la respuesta.
func (s *Source) Do(ctx context.Context, client *http.Client, build func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		token, err := s.Token(ctx)
		if err != nil {
			return nil, err
		}
		req, err := build()
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := client.Do(req.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusUnauthorized || attempt > 0 {
			return res, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxResponseBytes))
		_ = res.Body.Close()
		s.Invalidate(token)
	}
}

func (s *Source) fetch(ctx context.Context) (string, time.Duration, error) {
	payload, err := json.Marshal(map[string]string{"client_id": s.cfg.ClientID, "client_secret": s.cfg.ClientSecret})
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AuthURL+"/v1/token", bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("service token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		// El error de net/http lleva la URL, nunca el secreto (va en el cuerpo).
		return "", 0, fmt.Errorf("service token request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxResponseBytes))
		return "", 0, fmt.Errorf("service token: auth answered status %d", res.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponseBytes)).Decode(&out); err != nil {
		return "", 0, errors.New("service token: unreadable response from auth")
	}
	if out.AccessToken == "" || out.ExpiresIn <= 0 {
		return "", 0, errors.New("service token: auth returned an empty token")
	}
	return out.AccessToken, time.Duration(out.ExpiresIn) * time.Second, nil
}
