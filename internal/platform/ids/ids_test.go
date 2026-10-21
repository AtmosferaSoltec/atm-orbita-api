package ids

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIsVersion7(t *testing.T) {
	assert.EqualValues(t, 7, New().Version())
}

func TestParseAcceptsTheIdsAndroidGenerates(t *testing.T) {
	v4, err := Parse("3f1c9a52-8b2e-4d6f-9c1a-2b3c4d5e6f70")
	require.NoError(t, err)
	assert.EqualValues(t, 4, v4.Version())

	v7 := New()
	parsed, err := Parse(v7.String())
	require.NoError(t, err)
	assert.Equal(t, v7, parsed)
}

func TestParseRejectsAnythingElse(t *testing.T) {
	for _, text := range []string{"", "abc", "00000000-0000-0000-0000-000000000000", "3f1c9a52-8b2e"} {
		_, err := Parse(text)
		assert.Error(t, err, text)
	}
}
