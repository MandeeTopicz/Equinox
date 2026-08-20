package match

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Will the Fed cut rates in March?", "will-the-fed-cut-rates-in-march"},
		{"March FOMC: rate cut?", "march-fomc-rate-cut"},
		{"  leading/trailing spaces  ", "leading-trailing-spaces"},
		{"already-hyphenated", "already-hyphenated"},
		{"", ""},
		{"???", ""},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSlugifyTruncatesLongTitles(t *testing.T) {
	long := "this is a very long market title that keeps going and going and going and going and going"
	got := Slugify(long)
	if len(got) > maxSlugLength {
		t.Errorf("slug length %d exceeds max %d: %q", len(got), maxSlugLength, got)
	}
}

func TestUniqueSlug(t *testing.T) {
	used := map[string]bool{}

	first := UniqueSlug("Will the Fed cut rates?", used)
	if first != "will-the-fed-cut-rates" {
		t.Errorf("first slug = %q, want will-the-fed-cut-rates", first)
	}
	used[first] = true

	second := UniqueSlug("Will the Fed cut rates?", used)
	if second != "will-the-fed-cut-rates-2" {
		t.Errorf("second slug = %q, want will-the-fed-cut-rates-2", second)
	}
	used[second] = true

	third := UniqueSlug("Will the Fed cut rates?", used)
	if third != "will-the-fed-cut-rates-3" {
		t.Errorf("third slug = %q, want will-the-fed-cut-rates-3", third)
	}
}

func TestUniqueSlugEmptyTitle(t *testing.T) {
	used := map[string]bool{}
	if got := UniqueSlug("???", used); got != "event" {
		t.Errorf("UniqueSlug for an unslugifiable title = %q, want event", got)
	}
}
