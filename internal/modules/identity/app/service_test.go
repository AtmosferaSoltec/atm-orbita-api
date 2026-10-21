package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
)

// ---- puertos falsos -------------------------------------------------------

type memory struct {
	mu       sync.Mutex
	users    map[uuid.UUID]domain.User
	sessions map[uuid.UUID]*fakeSession
	tokens   []*fakeToken
}

type fakeSession struct {
	Session
	revoked string
}

type fakeToken struct {
	RefreshToken
	used bool
}

func newMemory() *memory {
	return &memory{users: map[uuid.UUID]domain.User{}, sessions: map[uuid.UUID]*fakeSession{}}
}

type fakeUsers struct{ m *memory }

func (f fakeUsers) Create(_ context.Context, user domain.User) error {
	for _, u := range f.m.users {
		if u.Email == user.Email {
			return domain.ErrEmailTaken
		}
	}
	f.m.users[user.ID] = user
	return nil
}

func (f fakeUsers) ByEmail(_ context.Context, email string) (domain.User, error) {
	for _, u := range f.m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (f fakeUsers) ByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	if u, ok := f.m.users[id]; ok {
		return u, nil
	}
	return domain.User{}, domain.ErrNotFound
}

func (fakeUsers) TouchLastLogin(context.Context, uuid.UUID) error { return nil }

type fakeSessions struct{ m *memory }

func (f fakeSessions) Create(_ context.Context, s Session) error {
	f.m.sessions[s.ID] = &fakeSession{Session: s}
	return nil
}

func (f fakeSessions) IsActive(_ context.Context, sessionID, userID uuid.UUID) (bool, error) {
	s, ok := f.m.sessions[sessionID]
	return ok && s.UserID == userID && s.revoked == "", nil
}

func (f fakeSessions) Revoke(_ context.Context, id uuid.UUID, reason string) error {
	if s, ok := f.m.sessions[id]; ok && s.revoked == "" {
		s.revoked = reason
	}
	return nil
}

func (fakeSessions) Touch(context.Context, uuid.UUID) error { return nil }

func (f fakeSessions) CreateRefreshToken(_ context.Context, t RefreshToken) error {
	f.m.tokens = append(f.m.tokens, &fakeToken{RefreshToken: t})
	return nil
}

func (f fakeSessions) LockRefreshToken(_ context.Context, hash []byte) (LockedRefreshToken, error) {
	for _, t := range f.m.tokens {
		if bytes.Equal(t.Hash, hash) {
			s := f.m.sessions[t.SessionID]
			return LockedRefreshToken{
				ID: t.ID, SessionID: t.SessionID, UserID: s.UserID, ExpiresAt: t.ExpiresAt,
				Used: t.used, SessionRevoked: s.revoked != "",
			}, nil
		}
	}
	return LockedRefreshToken{}, domain.ErrNotFound
}

func (f fakeSessions) MarkRefreshTokenUsed(_ context.Context, id uuid.UUID) error {
	for _, t := range f.m.tokens {
		if t.ID == id {
			t.used = true
		}
	}
	return nil
}

// fakeTx serializa como lo haría el bloqueo de la fila; no deshace (no hace falta aquí).
type fakeTx struct{ m *memory }

func (f fakeTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	f.m.mu.Lock()
	defer f.m.mu.Unlock()
	return fn(ctx)
}

type plainHasher struct{}

func (plainHasher) Hash(p string) (string, error)    { return "hash:" + p, nil }
func (plainHasher) Verify(p, h string) (bool, error) { return h == "hash:"+p, nil }

type fakeTokens struct{}

func (fakeTokens) Issue(c AccessClaims, _ time.Time) (string, error) {
	return c.UserID.String() + "|" + c.SessionID.String(), nil
}

func (fakeTokens) Verify(token string, _ time.Time) (AccessClaims, error) {
	user, session, ok := strings.Cut(token, "|")
	if !ok {
		return AccessClaims{}, auth.ErrUnauthorized
	}
	return AccessClaims{UserID: uuid.MustParse(user), SessionID: uuid.MustParse(session)}, nil
}

// ---- ayudantes -------------------------------------------------------------

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

var _ clock.Clock = (*testClock)(nil)

func newService(t *testing.T) (*Service, *memory, *testClock) {
	t.Helper()
	m := newMemory()
	c := &testClock{now: time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)}
	s, err := NewService(Deps{
		Users: fakeUsers{m}, Sessions: fakeSessions{m}, Hasher: plainHasher{}, Tokens: fakeTokens{},
		Tx: fakeTx{m}, Clock: c,
		Config: Config{AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: 30 * 24 * time.Hour},
	})
	require.NoError(t, err)
	return s, m, c
}

func register(t *testing.T, s *Service) AuthResult {
	t.Helper()
	result, err := s.Register(context.Background(), RegisterInput{Email: "Ana@Correo.com", Password: "vidanueva123"})
	require.NoError(t, err)
	return result
}

func fieldCodes(err error) []string {
	var v *domain.ValidationError
	if !errors.As(err, &v) {
		return nil
	}
	codes := make([]string, 0, len(v.Fields))
	for _, f := range v.Fields {
		codes = append(codes, f.Code)
	}
	return codes
}

// ---- pruebas ---------------------------------------------------------------

func TestRegisterValidatesInTheOrderOfTheApp(t *testing.T) {
	s, _, _ := newService(t)
	_, err := s.Register(context.Background(), RegisterInput{Email: "no-es-correo", Password: "corta", MainCurrency: "XXX"})
	assert.Equal(t, []string{domain.CodeEmailInvalid, domain.CodePasswordTooShort, domain.CodeCurrencyNotSupported}, fieldCodes(err))
}

func TestRegisterStoresTheEmailNormalizedAndOpensASession(t *testing.T) {
	s, m, _ := newService(t)
	result := register(t, s)
	assert.Equal(t, "ana@correo.com", result.User.Email)
	assert.NotEmpty(t, result.Tokens.AccessToken)
	assert.NotEmpty(t, result.Tokens.RefreshToken)
	assert.Equal(t, 15*time.Minute, result.Tokens.ExpiresIn)
	assert.Len(t, m.sessions, 1)
	// Solo se guarda el hash del token de renovación.
	assert.NotEqual(t, []byte(result.Tokens.RefreshToken), m.tokens[0].Hash)
}

func TestRegisterTwiceWithTheSameEmailFails(t *testing.T) {
	s, _, _ := newService(t)
	register(t, s)
	_, err := s.Register(context.Background(), RegisterInput{Email: "ANA@correo.com", Password: "otra-clave-1"})
	assert.ErrorIs(t, err, domain.ErrEmailTaken)
}

func TestRegistrationHooksGetTheChoicesAndCanStopTheSignUp(t *testing.T) {
	s, m, _ := newService(t)
	var got NewUser
	s.OnUserRegistered(func(_ context.Context, u NewUser) error { got = u; return nil })
	_, err := s.Register(context.Background(), RegisterInput{
		Email: "mx@correo.com", Password: "vidanueva123", MainCurrency: "MXN", Timezone: "America/Mexico_City",
	})
	require.NoError(t, err)
	assert.Equal(t, "MXN", got.MainCurrency)
	assert.Equal(t, "America/Mexico_City", got.Timezone)

	// Sin moneda ni zona válida: soles y Lima.
	_, err = s.Register(context.Background(), RegisterInput{Email: "pe@correo.com", Password: "vidanueva123", Timezone: "Marte/Base"})
	require.NoError(t, err)
	assert.Equal(t, "PEN", got.MainCurrency)
	assert.Equal(t, DefaultTimezone, got.Timezone)

	failure := errors.New("profile failed")
	s.OnUserRegistered(func(context.Context, NewUser) error { return failure })
	_, err = s.Register(context.Background(), RegisterInput{Email: "falla@correo.com", Password: "vidanueva123"})
	assert.ErrorIs(t, err, failure)
	_ = m
}

func TestWrongPasswordAndUnknownEmailGiveTheSameError(t *testing.T) {
	s, _, _ := newService(t)
	register(t, s)
	_, wrong := s.Login(context.Background(), LoginInput{Email: "ana@correo.com", Password: "equivocada"})
	_, unknown := s.Login(context.Background(), LoginInput{Email: "nadie@correo.com", Password: "vidanueva123"})
	assert.ErrorIs(t, wrong, domain.ErrInvalidCredentials)
	assert.ErrorIs(t, unknown, domain.ErrInvalidCredentials)
}

func TestLoginWaitsLongerAfterRepeatedFailures(t *testing.T) {
	s, _, c := newService(t)
	register(t, s)
	ctx := context.Background()
	for range failuresBeforeWait {
		_, err := s.Login(ctx, LoginInput{Email: "ana@correo.com", Password: "equivocada"})
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}
	_, err := s.Login(ctx, LoginInput{Email: "ana@correo.com", Password: "vidanueva123"})
	var tooMany *domain.TooManyAttemptsError
	require.ErrorAs(t, err, &tooMany)
	assert.Equal(t, time.Minute, tooMany.RetryAfter)

	// Pasado el minuto, la contraseña correcta entra y el contador vuelve a cero.
	c.now = c.now.Add(time.Minute)
	_, err = s.Login(ctx, LoginInput{Email: "ana@correo.com", Password: "vidanueva123"})
	assert.NoError(t, err)
}

func TestRefreshRotatesTheToken(t *testing.T) {
	s, _, _ := newService(t)
	first := register(t, s).Tokens
	second, err := s.Refresh(context.Background(), first.RefreshToken)
	require.NoError(t, err)
	assert.NotEqual(t, first.RefreshToken, second.RefreshToken)

	third, err := s.Refresh(context.Background(), second.RefreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, third.AccessToken)
}

func TestReusingAnOldRefreshTokenRevokesTheWholeSession(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	first := register(t, s).Tokens
	second, err := s.Refresh(ctx, first.RefreshToken)
	require.NoError(t, err)

	_, err = s.Refresh(ctx, first.RefreshToken)
	assert.ErrorIs(t, err, domain.ErrSessionExpired)

	// El token nuevo tampoco sirve ya, ni su access token.
	_, err = s.Refresh(ctx, second.RefreshToken)
	assert.ErrorIs(t, err, domain.ErrSessionExpired)
	_, err = s.Authenticate(ctx, second.AccessToken)
	assert.ErrorIs(t, err, auth.ErrUnauthorized)
}

func TestAnExpiredRefreshTokenDoesNotWork(t *testing.T) {
	s, _, c := newService(t)
	tokens := register(t, s).Tokens
	c.now = c.now.Add(30 * 24 * time.Hour)
	_, err := s.Refresh(context.Background(), tokens.RefreshToken)
	assert.ErrorIs(t, err, domain.ErrSessionExpired)
}

func TestLogoutEndsTheSessionAtOnce(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	tokens := register(t, s).Tokens

	principal, err := s.Authenticate(ctx, tokens.AccessToken)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, principal.UserID)

	require.NoError(t, s.Logout(ctx, tokens.RefreshToken))
	_, err = s.Refresh(ctx, tokens.RefreshToken)
	assert.ErrorIs(t, err, domain.ErrSessionExpired)
	_, err = s.Authenticate(ctx, tokens.AccessToken)
	assert.ErrorIs(t, err, auth.ErrUnauthorized)

	// Cerrar sesión con un token desconocido no es un error.
	assert.NoError(t, s.Logout(ctx, "unknown"))
}

func TestEachDeviceHasItsOwnSession(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	phone := register(t, s).Tokens
	tablet, err := s.Login(ctx, LoginInput{Email: "ana@correo.com", Password: "vidanueva123"})
	require.NoError(t, err)

	require.NoError(t, s.Logout(ctx, phone.RefreshToken))
	_, err = s.Authenticate(ctx, tablet.Tokens.AccessToken)
	assert.NoError(t, err)
}
