package goserver

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func Transliterate(text string) string {
	var mapped strings.Builder
	mapped.Grow(len(text))
	for _, r := range strings.ToLower(text) {
		if value, ok := transliterationMap[r]; ok {
			mapped.WriteString(value)
			continue
		}

		for _, decomposed := range norm.NFD.String(string(r)) {
			if value, ok := transliterationMap[decomposed]; ok {
				mapped.WriteString(value)
			} else {
				mapped.WriteRune(decomposed)
			}
		}
	}

	var out strings.Builder
	previousHyphen := false
	for _, r := range norm.NFD.String(strings.TrimSpace(mapped.String())) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			previousHyphen = false
		case unicode.IsSpace(r), r == '-':
			if !previousHyphen {
				out.WriteByte('-')
				previousHyphen = true
			}
		}
	}
	return out.String()
}

var transliterationMap = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "j", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "",
	'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	'ł': "l",
	'α': "a", 'β': "b", 'γ': "g", 'δ': "d", 'ε': "e", 'ζ': "z", 'η': "h",
	'θ': "th", 'ι': "i", 'κ': "k", 'λ': "l", 'μ': "m", 'ν': "n", 'ξ': "x",
	'ο': "o", 'π': "p", 'ρ': "r", 'σ': "s", 'ς': "s", 'τ': "t", 'υ': "y",
	'φ': "f", 'χ': "ch", 'ψ': "ps", 'ω': "o",
}
