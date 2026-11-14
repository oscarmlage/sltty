package emoji

import "testing"

func TestGlyph(t *testing.T) {
	for in, want := range map[string]string{
		"tomato":          "🍅",
		":floppy_disk:":   "💾",
		"+1":              "👍",
		"thumbsup":        "👍",
		"+1::skin-tone-3": "👍\U0001F3FC",
		"heart":           "❤️",
	} {
		if got, ok := Glyph(in); !ok || got != want {
			t.Errorf("Glyph(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := Glyph("clap-inclusive"); ok {
		t.Error("a custom emoji has no character")
	}
}

func TestName(t *testing.T) {
	for in, want := range map[string]string{"👍": "+1", "🍅": "tomato", "❤️": "heart", "❤": "heart", "☕": "coffee"} {
		if got, ok := Name(in); !ok || got != want {
			t.Errorf("Name(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestExpand(t *testing.T) {
	got := Expand("ok :+1::skin-tone-2: vamos :rocket: :pepe_happyclap: 10:30")
	want := "ok 👍\U0001F3FB vamos 🚀 :pepe_happyclap: 10:30"
	if got != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}
}
