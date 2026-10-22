package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
)

type fakeProfiles struct {
	profiles map[uuid.UUID]StoredProfile
	rates    []NewRate
	times    []time.Time
	now      time.Time
}

func newFake() *fakeProfiles {
	return &fakeProfiles{profiles: map[uuid.UUID]StoredProfile{}, now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeProfiles) Create(_ context.Context, id uuid.UUID, p StoredProfile) error {
	f.profiles[id] = p
	return nil
}

func (f *fakeProfiles) Get(_ context.Context, id uuid.UUID) (StoredProfile, error) {
	p, ok := f.profiles[id]
	if !ok {
		return StoredProfile{}, domain.ErrNotFound
	}
	return p, nil
}

func (f *fakeProfiles) Lock(ctx context.Context, id uuid.UUID) (StoredProfile, error) {
	return f.Get(ctx, id)
}

func (f *fakeProfiles) Update(_ context.Context, id uuid.UUID, p StoredProfile) error {
	f.profiles[id] = p
	return nil
}

func (f *fakeProfiles) InsertRate(_ context.Context, r NewRate) error {
	f.now = f.now.Add(time.Minute)
	f.rates = append(f.rates, r)
	f.times = append(f.times, f.now)
	return nil
}

func (f *fakeProfiles) LatestRate(_ context.Context, id uuid.UUID, a, b string) (StoredRate, bool, error) {
	for i := len(f.rates) - 1; i >= 0; i-- {
		r := f.rates[i]
		if r.UserID == id && ((r.From == a && r.To == b) || (r.From == b && r.To == a)) {
			return StoredRate{From: r.From, To: r.To, Rate: r.Rate, CreatedAt: f.times[i]}, true, nil
		}
	}
	return StoredRate{}, false, nil
}

type noTx struct{}

func (noTx) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

func setup(t *testing.T, main string) (*Service, *fakeProfiles, uuid.UUID) {
	t.Helper()
	f := newFake()
	s := NewService(f, noTx{}, clock.Fixed{At: time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)})
	id := uuid.New()
	require.NoError(t, s.CreateForNewUser(context.Background(), id, main, "America/Lima"))
	return s, f, id
}

func text(s string) *string { return &s }

func fieldCodes(err error) []string {
	var v *domain.ValidationError
	if !errors.As(err, &v) {
		return nil
	}
	var codes []string
	for _, f := range v.Fields {
		codes = append(codes, f.Code)
	}
	return codes
}

func TestANewUserHasNoExchangeRate(t *testing.T) {
	s, _, id := setup(t, "PEN")
	settings, err := s.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "PEN", settings.Main)
	assert.Equal(t, "USD", settings.Secondary)
	assert.Equal(t, "PEN", settings.DisplayCurrency)
	assert.False(t, settings.IsConfigured())
	assert.Nil(t, settings.RateUpdatedAt)
}

func TestAUserWhoStartsInDollarsGetsSolesAsSecondary(t *testing.T) {
	s, _, id := setup(t, "USD")
	settings, _ := s.Get(context.Background(), id)
	assert.Equal(t, "PEN", settings.Secondary)
}

func TestAPairIsNeverSavedWithoutARateAboveZero(t *testing.T) {
	s, f, id := setup(t, "PEN")
	ctx := context.Background()
	for _, rate := range []*string{nil, text(""), text("0"), text("0.000000")} {
		_, err := s.SetFx(ctx, id, SetFxInput{Main: "PEN", Secondary: "USD", Rate: rate})
		assert.Equal(t, []string{domain.CodeRateRequired}, fieldCodes(err))
	}
	_, err := s.SetFx(ctx, id, SetFxInput{Main: "PEN", Secondary: "USD", Rate: text("3,20")})
	assert.Equal(t, []string{domain.CodeInvalidFormat}, fieldCodes(err))
	assert.Empty(t, f.rates)
}

func TestSetFxValidatesInOrder(t *testing.T) {
	s, _, id := setup(t, "PEN")
	_, err := s.SetFx(context.Background(), id, SetFxInput{Main: "XXX", Secondary: "XXX", Rate: nil})
	assert.Equal(t, []string{
		domain.CodeCurrencyNotSupported, domain.CodeCurrencyNotSupported, domain.CodeSameCurrency, domain.CodeRateRequired,
	}, fieldCodes(err))
}

func TestSettingTheRateKeepsTheHistory(t *testing.T) {
	s, f, id := setup(t, "PEN")
	ctx := context.Background()
	_, err := s.SetFx(ctx, id, SetFxInput{Main: "PEN", Secondary: "USD", Rate: text("3.20")})
	require.NoError(t, err)
	settings, err := s.SetFx(ctx, id, SetFxInput{Main: "PEN", Secondary: "USD", Rate: text("3.75")})
	require.NoError(t, err)
	assert.Equal(t, "3.75", settings.Rate.String())
	assert.Len(t, f.rates, 2)
}

func TestSwappingThePairKeepsTheRateAsItsInverse(t *testing.T) {
	s, _, id := setup(t, "PEN")
	ctx := context.Background()
	_, err := s.SetFx(ctx, id, SetFxInput{Main: "PEN", Secondary: "USD", Rate: text("3.20")})
	require.NoError(t, err)
	// La app manda el par al revés con el inverso; el más reciente manda.
	settings, err := s.SetFx(ctx, id, SetFxInput{Main: "USD", Secondary: "PEN", Rate: text("0.3125")})
	require.NoError(t, err)
	assert.Equal(t, "USD", settings.Main)
	assert.Equal(t, "0.3125", settings.Rate.String())
	// La vista estaba en soles, que sigue en el par.
	assert.Equal(t, "PEN", settings.DisplayCurrency)
}

func TestAnOlderRateStoredTheOtherWayIsReadAsItsInverse(t *testing.T) {
	s, f, id := setup(t, "PEN")
	f.rates = append(f.rates, NewRate{UserID: id, From: "PEN", To: "USD", Rate: decimal.RequireFromString("0.3125")})
	f.times = append(f.times, f.now)
	settings, err := s.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "3.2", settings.Rate.String())
}

func TestChangingToAnotherPairMovesTheDisplayToTheMain(t *testing.T) {
	s, _, id := setup(t, "PEN")
	ctx := context.Background()
	_, _ = s.SetFx(ctx, id, SetFxInput{Main: "PEN", Secondary: "USD", Rate: text("3.20")})
	_, err := s.Update(ctx, id, UpdateInput{DisplayCurrency: text("USD")})
	require.NoError(t, err)

	settings, err := s.SetFx(ctx, id, SetFxInput{Main: "MXN", Secondary: "EUR", Rate: text("21.5")})
	require.NoError(t, err)
	assert.Equal(t, "MXN", settings.DisplayCurrency)
}

func TestTheSecondaryCannotBeShownWithoutARate(t *testing.T) {
	s, _, id := setup(t, "PEN")
	ctx := context.Background()
	_, err := s.Update(ctx, id, UpdateInput{DisplayCurrency: text("USD")})
	assert.ErrorIs(t, err, domain.ErrFxNotConfigured)

	_, err = s.Update(ctx, id, UpdateInput{DisplayCurrency: text("EUR")})
	assert.Equal(t, []string{domain.CodeCurrencyNotInPair}, fieldCodes(err))
}

func TestTheTimeZoneCanChangeButMustBeReal(t *testing.T) {
	s, _, id := setup(t, "PEN")
	ctx := context.Background()
	settings, err := s.Update(ctx, id, UpdateInput{Timezone: text("America/Bogota")})
	require.NoError(t, err)
	assert.Equal(t, "America/Bogota", settings.Timezone)

	_, err = s.Update(ctx, id, UpdateInput{Timezone: text("Marte/Base")})
	assert.Equal(t, []string{domain.CodeTimezoneInvalid}, fieldCodes(err))
}

func TestTodayFollowsTheTimeZoneOfTheProfile(t *testing.T) {
	s, _, id := setup(t, "PEN")
	// El reloj marca la 01:00 UTC del 3 oct: en Lima todavía es 2 oct.
	today, err := s.Today(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-02", today.String())
}

func TestOnlyTheMainCurrencyIsUsableWithoutARate(t *testing.T) {
	s, _, id := setup(t, "PEN")
	ctx := context.Background()
	assert.NoError(t, s.RequireUsable(ctx, id, "PEN"))
	assert.ErrorIs(t, s.RequireUsable(ctx, id, "USD"), domain.ErrFxNotConfigured)
	_, _ = s.SetFx(ctx, id, SetFxInput{Main: "PEN", Secondary: "USD", Rate: text("3.20")})
	assert.NoError(t, s.RequireUsable(ctx, id, "USD"))
}
