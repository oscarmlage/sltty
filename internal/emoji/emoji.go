// Package emoji turns Slack's emoji names (:tada:, +1::skin-tone-3) into
// characters and back. Custom emoji are not here: they are the workspace's
// and come as pictures.
package emoji

//go:generate go run gen.go

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// tones are Slack's skin-tone-2 … skin-tone-6, the Fitzpatrick modifiers.
var tones = map[string]rune{
	"skin-tone-2": 0x1F3FB, "skin-tone-3": 0x1F3FC, "skin-tone-4": 0x1F3FD,
	"skin-tone-5": 0x1F3FE, "skin-tone-6": 0x1F3FF,
}

// Glyph returns the character for a Slack emoji name, with or without its
// colons and with an optional ::skin-tone-N.
func Glyph(name string) (string, bool) {
	name = strings.Trim(name, ":")
	base, tone, _ := strings.Cut(name, "::")
	g, ok := glyphs[base]
	if !ok {
		return "", false
	}
	if mod, ok := tones[tone]; ok {
		// The modifier goes right after the first character, in place of
		// its presentation selector: 👍🏽, 🙋🏻‍♂️.
		r, n := utf8.DecodeRuneInString(g)
		rest := strings.TrimPrefix(g[n:], "\uFE0F")
		g = string(r) + string(mod) + rest
	}
	return g, true
}

// Names is every emoji name Slack knows, sorted: aliases too ("+1",
// "thumbsup").
var Names = sync.OnceValue(func() []string { return slices.Sorted(maps.Keys(glyphs)) })

// Name returns the Slack name of an emoji character, for the APIs that
// take names (reactions.add).
func Name(glyph string) (string, bool) {
	if n, ok := names[glyph]; ok {
		return n, true
	}
	n, ok := names[glyph+"\uFE0F"]
	if !ok {
		n, ok = names[strings.TrimSuffix(glyph, "\uFE0F")]
	}
	return n, ok
}

// Shortcode finds :name: in text, skin tone included.
var Shortcode = regexp.MustCompile(`:[a-z0-9_+'-]+(?:::skin-tone-[2-6])?:`)

// Expand writes the standard :name: emoji of text as characters. Custom
// and unknown names stay as they are.
func Expand(text string) string {
	if !strings.Contains(text, ":") {
		return text
	}
	return Shortcode.ReplaceAllStringFunc(text, func(s string) string {
		if g, ok := Glyph(s); ok {
			return g
		}
		return s
	})
}
