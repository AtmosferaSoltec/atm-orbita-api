// Package text limpia y compara nombres escritos por las personas.
package text

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	// La misma clase que \s en Java (Kotlin): solo espacios ASCII.
	whitespace    = regexp.MustCompile(`[ \t\n\x0B\f\r]+`)
	combiningMark = regexp.MustCompile(`\p{M}+`)
)

// Ocupa el lugar de la ñ mientras se quitan las tildes; no se puede escribir.
const enyePlaceholder = '\u0001'

// CleanName es el nombre como se guarda y se muestra: sin espacios al inicio ni
// al final y con espacios simples; lo demás, tal como lo escribió la persona.
func CleanName(name string) string {
	return whitespace.ReplaceAllString(strings.TrimFunc(name, isKotlinWhitespace), " ")
}

// NormalizeName es la clave con que se comparan dos nombres: minúsculas, sin
// tildes y con espacios simples. "Alimentación", "alimentacion" y
// "ALIMENTACIÓN" dan la misma clave. La ñ es una letra propia, no una n con
// tilde, así que se conserva: "Año" y "Ano" son nombres distintos.
//
// Debe dar exactamente lo mismo que normalizeName de Android
// (domain/model/Names.kt): la app avisa antes de enviar y el API decide.
func NormalizeName(name string) string {
	// Primero compuesta, para proteger también una ñ escrita como n + tilde.
	composed := norm.NFC.String(strings.ToLower(CleanName(name)))
	composed = strings.ReplaceAll(composed, "ñ", string(enyePlaceholder))
	withoutAccents := combiningMark.ReplaceAllString(norm.NFD.String(composed), "")
	return norm.NFC.String(strings.ReplaceAll(withoutAccents, string(enyePlaceholder), "ñ"))
}

// isKotlinWhitespace reproduce Char.isWhitespace() de Kotlin, que usa String.trim():
// Character.isWhitespace o Character.isSpaceChar de Java.
func isKotlinWhitespace(r rune) bool {
	switch {
	case r == '\u0085':
		return false
	case r >= '\u001C' && r <= '\u001F':
		return true
	default:
		return unicode.IsSpace(r)
	}
}
