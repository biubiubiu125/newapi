package common

import "strings"

// foreignErrorWords are single English tokens that are error text, not a
// brand or format name such as JSON. A Chinese sentence may keep one brand.
var foreignErrorWords = map[string]struct{}{
	"timeout":      {},
	"refused":      {},
	"connection":   {},
	"failed":       {},
	"failure":      {},
	"error":        {},
	"errors":       {},
	"dial":         {},
	"connect":      {},
	"invalid":      {},
	"unexpected":   {},
	"exception":    {},
	"denied":       {},
	"unreachable":  {},
	"reset":        {},
	"closed":       {},
	"eof":          {},
	"broken":       {},
	"pipe":         {},
	"deadline":     {},
	"exceeded":     {},
	"forbidden":    {},
	"unauthorized": {},
	"internal":     {},
	"upstream":     {},
	"gateway":      {},
}

// SanitizeChineseConsoleText keeps Chinese clauses and drops English upstream
// text glued on with a colon or mixed into the same sentence. A single brand
// or format token such as "不是合法 JSON" stays. Text with no Han is empty so
// the caller can use its own fallback.
func SanitizeChineseConsoleText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || !containsHan(text) {
		return ""
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == ':' || r == '：' || r == ';' || r == '；' || r == '\n' || r == '\r'
	})
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned := cleanChineseClause(part)
		if cleaned == "" || !containsHan(cleaned) {
			continue
		}
		kept = append(kept, cleaned)
	}
	return strings.Join(kept, "：")
}

func cleanChineseClause(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || !containsHan(text) {
		return ""
	}
	words := latinWords(text)
	switch {
	case len(words) >= 2 && hasLongLatinWord(words):
		return collapseSpaces(dropASCIINoise(text))
	case hasForeignErrorWord(words):
		return collapseSpaces(dropForeignErrorWords(text))
	default:
		return text
	}
}

func latinWords(text string) []string {
	words := make([]string, 0, 4)
	run := make([]rune, 0, 8)
	flush := func() {
		if len(run) == 0 {
			return
		}
		words = append(words, strings.ToLower(string(run)))
		run = run[:0]
	}
	for _, r := range text {
		if isLatinLetter(r) {
			run = append(run, r)
			continue
		}
		flush()
	}
	flush()
	return words
}

func hasLongLatinWord(words []string) bool {
	for _, word := range words {
		if len(word) >= 4 {
			return true
		}
	}
	return false
}

func hasForeignErrorWord(words []string) bool {
	for _, word := range words {
		if _, ok := foreignErrorWords[word]; ok {
			return true
		}
	}
	return false
}

func isLatinLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func dropASCIINoise(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case isLatinLetter(r), r >= '0' && r <= '9':
			b.WriteByte(' ')
		case r < 128 && r != ' ' && r != '\t':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func dropForeignErrorWords(text string) string {
	var b strings.Builder
	runes := []rune(text)
	for i := 0; i < len(runes); {
		if !isLatinLetter(runes[i]) {
			b.WriteRune(runes[i])
			i++
			continue
		}
		j := i
		for j < len(runes) && isLatinLetter(runes[j]) {
			j++
		}
		word := strings.ToLower(string(runes[i:j]))
		if _, drop := foreignErrorWords[word]; drop {
			b.WriteByte(' ')
		} else {
			b.WriteString(string(runes[i:j]))
		}
		i = j
	}
	return b.String()
}

func collapseSpaces(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
