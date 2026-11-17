package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The widths kitty's wcswidth gives (kitty 0.50): the layout must agree.
func TestTermWidthsMatchKitty(t *testing.T) {
	for in, kitty := range map[string]int{
		"☝🏽": 2, "✍🏼": 2, "🏋🏼\u200d♀️": 2, "n\uFE0F": 1, "▶️": 2, "⚠️": 2, "👍🏽": 2, "🔒": 2, "·": 1, "plain": 5,
	} {
		if got := ansi.StringWidth(termWidths(in)); got != kitty {
			t.Errorf("%q: %d wide here, %d in kitty", in, got, kitty)
		}
	}
	if s := "no emoji here"; termWidths(s) != s {
		t.Fatal("plain text changed")
	}
	if got := fit("x ☝🏽 y", 10); ansi.StringWidth(got) != 10 {
		t.Fatalf("fit: %d wide", ansi.StringWidth(got))
	}
}
