package dpty

// MaxSessionNameLen is the longest accepted session name.
const MaxSessionNameLen = 64

// IsValidSessionName reports whether s is a syntactically valid session
// alias.
//
// Names must be 1-[MaxSessionNameLen] characters using only ASCII
// alphanumerics, dot, dash, or underscore. The constraints keep names
// safe to embed in URL paths and on disk without escaping.
func IsValidSessionName(s string) bool {
	if len(s) == 0 || len(s) > MaxSessionNameLen {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
