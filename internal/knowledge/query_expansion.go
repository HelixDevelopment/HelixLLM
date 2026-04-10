package knowledge

import "strings"

// codeSynonyms maps common programming terms to their synonyms for query
// expansion. This enables keyword search to find relevant results even when
// the query uses different terminology than the indexed documents.
var codeSynonyms = map[string][]string{
	"function": {"method", "def", "func", "procedure"},
	"class":    {"type", "struct", "object", "interface"},
	"error":    {"exception", "bug", "failure", "issue"},
	"database": {"db", "sql", "postgres", "sqlite"},
	"api":      {"endpoint", "route", "handler", "controller"},
	"import":   {"require", "include", "use", "dependency"},
	"test":     {"spec", "assert", "verify", "check"},
	"async":    {"goroutine", "concurrent", "parallel", "await"},
}

// ExpandQuery returns the original query plus expanded variants created by
// substituting recognized programming terms with their synonyms. If no
// synonym terms are found in the query, only the original is returned.
//
// The returned slice always has the original query as the first element.
func ExpandQuery(query string) []string {
	results := []string{query}
	lowerQuery := strings.ToLower(query)
	words := strings.Fields(lowerQuery)

	// Build a set of words for fast lookup.
	wordSet := make(map[string]bool, len(words))
	for _, w := range words {
		wordSet[w] = true
	}

	// For each synonym group, if a key term appears in the query, generate
	// expanded queries by replacing that term with each synonym.
	for term, synonyms := range codeSynonyms {
		if !wordSet[term] {
			continue
		}
		for _, syn := range synonyms {
			expanded := replaceTermIgnoreCase(query, term, syn)
			if expanded != query {
				results = append(results, expanded)
			}
		}
	}

	return results
}

// replaceTermIgnoreCase replaces whole-word occurrences of old with new in s,
// performing a case-insensitive match. Only the first occurrence is replaced
// to keep expanded queries focused.
func replaceTermIgnoreCase(s, old, replacement string) string {
	lower := strings.ToLower(s)
	lowerOld := strings.ToLower(old)

	idx := -1
	start := 0
	for {
		pos := strings.Index(lower[start:], lowerOld)
		if pos < 0 {
			break
		}
		absPos := start + pos

		// Check word boundaries: the character before and after the match
		// must be a non-alphanumeric character or string boundary.
		before := absPos == 0 || !isAlphaNum(lower[absPos-1])
		after := absPos+len(lowerOld) >= len(lower) || !isAlphaNum(lower[absPos+len(lowerOld)])
		if before && after {
			idx = absPos
			break
		}
		start = absPos + len(lowerOld)
	}

	if idx < 0 {
		return s
	}

	return s[:idx] + replacement + s[idx+len(old):]
}

// isAlphaNum returns true if b is an ASCII letter or digit.
func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
