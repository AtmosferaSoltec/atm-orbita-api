package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestQuotaLimiterAllowsTheLimitPerKeyAndFreesItAfterTheWindow(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	limiter := NewQuotaLimiter(2, time.Hour)
	limiter.now = func() time.Time { return now }

	_, ok := limiter.Allow("a")
	assert.True(t, ok)
	now = now.Add(10 * time.Minute)
	_, ok = limiter.Allow("a")
	assert.True(t, ok)

	retry, ok := limiter.Allow("a")
	assert.False(t, ok)
	assert.Equal(t, 50*time.Minute, retry, "the oldest use frees up first")
	_, ok = limiter.Allow("b")
	assert.True(t, ok, "keys are independent")

	now = now.Add(51 * time.Minute)
	_, ok = limiter.Allow("a")
	assert.True(t, ok)
	_, ok = limiter.Allow("a")
	assert.False(t, ok, "the second use is still inside the window")
}
