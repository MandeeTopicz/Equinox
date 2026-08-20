package match

import (
	"fmt"
	"strings"
)

// maxSlugLength bounds how long a generated event id can be, so a long
// market title doesn't produce an unwieldy --event value.
const maxSlugLength = 60

// Slugify turns a market title into a URL/CLI-friendly event id fragment:
// lowercase, non-alphanumeric runs collapsed to a single hyphen, trimmed.
func Slugify(s string) string {
	var b strings.Builder
	lastHyphen := true // suppresses a leading hyphen
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")

	if len(slug) > maxSlugLength {
		slug = strings.Trim(slug[:maxSlugLength], "-")
	}
	return slug
}

// UniqueSlug slugifies title and, if the result already appears in used,
// appends "-2", "-3", etc. until it's unique. It does not add the result to
// used — callers do that once they've committed to it.
func UniqueSlug(title string, used map[string]bool) string {
	base := Slugify(title)
	if base == "" {
		base = "event"
	}

	candidate := base
	for n := 2; used[candidate]; n++ {
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
	return candidate
}
