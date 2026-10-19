package api

import (
	"strings"
	"unicode"
)

// accents covers the letters Spanish (and the odd brand name) uses; the
// site's own slugs drop them the same way.
var accents = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "ö", "o", "ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c", "º", "o", "ª", "a")

// fold lowercases and strips accents, so "campaña" matches "CAMPAÑA" and
// "campana".
func fold(s string) string { return accents.Replace(strings.ToLower(strings.TrimSpace(s))) }

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range fold(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}
