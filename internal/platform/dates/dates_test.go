package dates

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
)

func TestTodayFollowsTheTimeZoneOfThePerson(t *testing.T) {
	lima, err := LoadLocation("America/Lima")
	require.NoError(t, err)

	// 20:00 del 2 oct 2026 en Lima es la 01:00 UTC del 3 oct.
	at := clock.Fixed{At: time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)}
	assert.Equal(t, "2026-10-02", Today(at, lima).String())
	assert.Equal(t, "2026-10-03", Today(at, time.UTC).String())
}

func TestParseOnlyAcceptsRealDates(t *testing.T) {
	date, err := Parse("2026-10-02")
	require.NoError(t, err)
	assert.Equal(t, New(2026, time.October, 2), date)

	for _, text := range []string{"2026-02-30", "02/10/2026", "2026-10-2", "2026-10-02T00:00:00Z", ""} {
		_, err := Parse(text)
		assert.ErrorIs(t, err, ErrInvalidDate, text)
	}
}

func TestLoadLocationRejectsUnknownZones(t *testing.T) {
	for _, name := range []string{"", "Local", "Lima", "America/Nowhere"} {
		_, err := LoadLocation(name)
		assert.Error(t, err, name)
	}
}

func TestDatesTravelAsText(t *testing.T) {
	out, err := json.Marshal(struct {
		On LocalDate `json:"occurred_on"`
	}{New(2026, time.September, 1)})
	require.NoError(t, err)
	assert.JSONEq(t, `{"occurred_on":"2026-09-01"}`, string(out))

	var in struct {
		On LocalDate `json:"occurred_on"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"occurred_on":"2026-09-30"}`), &in))
	assert.True(t, in.On.After(New(2026, time.September, 29)))
	assert.Equal(t, New(2026, time.October, 1), in.On.AddDays(1))
}
