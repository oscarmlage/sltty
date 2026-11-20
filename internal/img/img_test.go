package img

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDetect(t *testing.T) {
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	for _, c := range []struct {
		cfg  string
		env  func(string) string
		want Mode
	}{
		{"", env("TERM_PROGRAM", "ghostty"), Kitty},
		{"auto", env("KITTY_WINDOW_ID", "1"), Kitty},
		{"", env("TERM_PROGRAM", "Apple_Terminal"), HalfBlock},
		{"", env("TMUX", "x", "TERM_PROGRAM", "ghostty"), HalfBlock},
		{"kitty", env("TMUX", "x"), Kitty},
		{"off", env("TERM_PROGRAM", "ghostty"), Off},
	} {
		if got, err := Detect(c.cfg, c.env); err != nil || got != c.want {
			t.Errorf("Detect(%q): %v %v, want %v", c.cfg, got, err, c.want)
		}
	}
	if _, err := Detect("sixel", env()); err == nil {
		t.Error("an unknown mode passed")
	}
}

func TestCellsAndUpload(t *testing.T) {
	ls := Cells(0x010203, 4, 3)
	if len(ls) != 3 || ansi.StringWidth(ls[2]) != 4 || !strings.Contains(ls[0], "38;2;1;2;3") {
		t.Fatalf("cells %q", ls)
	}
	up := Upload(7, make([]byte, 10000), 4, 3)
	if !strings.Contains(up, "a=t,f=100,t=d,i=7,U=1") || strings.Count(up, "\x1b_G") != 1+4 {
		t.Fatalf("upload: %d chunks", strings.Count(up, "\x1b_G"))
	}
	if strings.Contains(up, "t=f") || strings.Contains(up, "t=t") {
		t.Fatal("images must go to the terminal directly, never through a file")
	}
}

func TestFitAndHalfBlocks(t *testing.T) {
	if c, r := Fit(640, 360, 40, 14); c != 40 || r != 11 {
		t.Fatalf("fit 640×360: %d×%d", c, r)
	}
	if c, r := Fit(100, 1000, 40, 14); r != 14 || c > 3 {
		t.Fatalf("fit tall: %d×%d", c, r)
	}
	// A viewer grows a small picture to the box, aspect kept.
	if c, r := Fill(64, 32, 100, 40); c != 100 || r != 25 {
		t.Fatalf("fill wide: %d×%d", c, r)
	}
	if c, r := Fill(300, 600, 100, 40); r != 40 || c != 40 {
		t.Fatalf("fill tall: %d×%d", c, r)
	}
	if c, r := Fill(0, 0, 100, 40); c != 0 || r != 0 {
		t.Fatalf("fill unknown: %d×%d", c, r)
	}
	// A taller cell (7 × 17 pixels) takes fewer rows: no blank below.
	tall := Cell{W: 7, H: 17}
	if c, r := tall.Fit(640, 360, 40, 14); c != 40 || r != 9 {
		t.Fatalf("fit, tall cells: %d×%d", c, r)
	}
	if c, r := tall.Fill(300, 600, 100, 40); r != 40 || c != 49 {
		t.Fatalf("fill, tall cells: %d×%d", c, r)
	}
	if c, r := (Cell{}).Fit(640, 360, 40, 14); c != 40 || r != 11 {
		t.Fatalf("an unknown cell is not the default: %d×%d", c, r)
	}
	im := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 2 {
		for x := range 4 {
			im.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	ls := HalfBlocks(im, 2, 1)
	if len(ls) != 1 || ansi.StringWidth(ls[0]) != 2 || !strings.Contains(ls[0], "38;2;255;0;0m\x1b[48;2;0;0;0m▀") {
		t.Fatalf("half blocks %q", ls)
	}
}
