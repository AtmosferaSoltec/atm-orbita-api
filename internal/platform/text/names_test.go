package text

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Vectores de atm-orbita-android (MoneyRulesTest.kt, DemoRepositoriesTest.kt) y
// de ORBITA_SPEC §7.11.

func TestCaseAndAccentsAreIgnored(t *testing.T) {
	key := NormalizeName("Alimentación")
	assert.Equal(t, "alimentacion", key)
	assert.Equal(t, key, NormalizeName("alimentacion"))
	assert.Equal(t, key, NormalizeName("ALIMENTACIÓN"))
	assert.Equal(t, "pinguino", NormalizeName("Pingüino"))
}

func TestTheEnyeStays(t *testing.T) {
	assert.Equal(t, "año", NormalizeName("Año"))
	assert.NotEqual(t, NormalizeName("Año"), NormalizeName("Ano"))
	// Una ñ escrita como n + tilde combinante también se conserva.
	assert.Equal(t, "año", NormalizeName("Año"))
	assert.Equal(t, "año", NormalizeName("AÑO"))
}

func TestSpacesAreIgnoredAtTheEndsAndCollapsedInside(t *testing.T) {
	assert.Equal(t, "otros ingresos", NormalizeName("  Otros   ingresos "))
	assert.Equal(t, NormalizeName("Otros ingresos"), NormalizeName("Otros\tingresos"))
}

func TestCleanNameKeepsWhatWasTyped(t *testing.T) {
	assert.Equal(t, "Alimentación", CleanName("  Alimentación "))
	assert.Equal(t, "Otros ingresos", CleanName("Otros   ingresos"))
}
