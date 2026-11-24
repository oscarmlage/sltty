// Package img draws images in the terminal, the way slk does: kitty's
// graphics protocol with unicode placeholders where the terminal has it
// (kitty, Ghostty, WezTerm), half blocks (▀ with two colors a cell)
// anywhere else.
//
// With placeholders the image is uploaded once, out of band, and the screen
// only holds ordinary text cells (U+10EEEE plus two diacritics for the row
// and the column, the foreground color naming the image), so the TUI lays
// it out, scrolls and diffs it like any other text.
//
// Images travel to the terminal directly (t=d), never through a file, and
// are kept only in memory.
package img

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // Decode reads them
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"
)

type Mode int

const (
	Off Mode = iota
	HalfBlock
	Kitty
)

func (m Mode) String() string {
	switch m {
	case HalfBlock:
		return "halfblock"
	case Kitty:
		return "kitty"
	}
	return "off"
}

// Modes are the values of the "images" config key.
var Modes = []string{"auto", "kitty", "halfblock", "off"}

// Detect picks the mode for the config value: "auto" (or empty) looks at
// the terminal.
func Detect(cfg string, getenv func(string) string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(cfg)) {
	case "off":
		return Off, nil
	case "halfblock":
		return HalfBlock, nil
	case "kitty":
		return Kitty, nil
	case "", "auto":
	default:
		return Off, fmt.Errorf("images: %q is none of %s", cfg, strings.Join(Modes, ", "))
	}
	switch {
	case getenv("TMUX") != "":
		// kitty images need allow-passthrough in tmux: only when asked for
		return HalfBlock, nil
	case getenv("KITTY_WINDOW_ID") != "", getenv("TERM") == "xterm-kitty",
		getenv("TERM_PROGRAM") == "ghostty", getenv("TERM_PROGRAM") == "WezTerm":
		return Kitty, nil
	}
	return HalfBlock, nil
}

// Placeholder is the rune of kitty's unicode placeholders.
const Placeholder = '\U0010EEEE'

// diacritics number the rows and columns of a placeholder (from kitty's
// rowcolumn-diacritics.txt): the i-th one is row or column i.
var diacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A,
	0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357, 0x035B, 0x0363, 0x0364, 0x0365,
	0x0366, 0x0367, 0x0368, 0x0369, 0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F,
	0x0483, 0x0484, 0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1, 0x05A8, 0x05A9,
	0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611, 0x0612, 0x0613, 0x0614, 0x0615,
	0x0616, 0x0617, 0x0657, 0x0658, 0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6,
	0x06D7, 0x06D8, 0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733, 0x0735, 0x0736,
	0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743, 0x0745, 0x0747, 0x0749, 0x074A,
	0x07EB, 0x07EC, 0x07ED, 0x07EE, 0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817,
	0x0818, 0x0819, 0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
}

// MaxCells is the largest side, in cells, a placeholder can number.
var MaxCells = len(diacritics)

// Upload sends a PNG and creates its cell-sized placement for later Unicode
// placeholder cells. Terminals such as Ghostty require that placement before
// they render the placeholders. In tmux it is wrapped for passthrough.
func Upload(id uint32, pngData []byte, cols, rows int) string {
	const chunk = 4096
	payload := base64.StdEncoding.EncodeToString(pngData)
	var b strings.Builder
	b.WriteString(wrap(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)))
	for i := 0; i < len(payload); i += chunk {
		end, more := min(i+chunk, len(payload)), 1
		if end == len(payload) {
			more = 0
		}
		hdr := fmt.Sprintf("m=%d", more)
		if i == 0 {
			hdr = fmt.Sprintf("a=T,f=100,t=d,i=%d,U=1,c=%d,r=%d,q=2,m=%d", id, cols, rows, more)
		}
		b.WriteString(wrap("\x1b_G" + hdr + ";" + payload[i:end] + "\x1b\\"))
	}
	return b.String()
}

// Delete is the escape sequence that frees image id in the terminal.
func Delete(id uint32) string { return wrap(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)) }

func wrap(seq string) string {
	if os.Getenv("TMUX") == "" {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// Cells are the placeholder lines of image id: rows lines of cols cells.
// Every cell carries its row and column, so any cell stands on its own.
func Cells(id uint32, cols, rows int) []string {
	sgr := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", byte(id>>16), byte(id>>8), byte(id))
	out := make([]string, rows)
	for r := range rows {
		var b strings.Builder
		b.WriteString(sgr)
		for c := range cols {
			b.WriteRune(Placeholder)
			b.WriteRune(diacritics[r])
			b.WriteRune(diacritics[c])
		}
		b.WriteString("\x1b[39m")
		out[r] = b.String()
	}
	return out
}

// IsPlaceholder tells a placeholder cell's content.
func IsPlaceholder(cell string) bool { return strings.HasPrefix(cell, string(Placeholder)) }

// Cell is a terminal cell's shape, in pixels or any unit: about twice as
// high as wide, DefaultCell, until the terminal says (CSI 16 t).
type Cell struct{ W, H int }

var DefaultCell = Cell{W: 1, H: 2}

func (c Cell) valid() Cell {
	if c.W <= 0 || c.H <= 0 {
		return DefaultCell
	}
	return c
}

// rows is how high a w × h image cols wide is, aspect kept.
func (c Cell) rows(w, h, cols int) int {
	return max(1, (2*cols*h*c.W+w*c.H)/(2*w*c.H))
}

// Fit is the cells an image of w × h pixels takes at most maxCols wide and
// maxRows high.
func (c Cell) Fit(w, h, maxCols, maxRows int) (cols, rows int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	c = c.valid()
	cols = min(maxCols, max(1, w/8)) // small images stay small
	rows = c.rows(w, h, cols)
	if rows > maxRows {
		rows = maxRows
		cols = max(1, min(cols, rows*w*c.H/(h*c.W)))
	}
	return cols, rows
}

// Fill is the largest cells an image of w × h pixels takes within maxCols ×
// maxRows, small ones grown: a viewer's picture.
func (c Cell) Fill(w, h, maxCols, maxRows int) (cols, rows int) {
	if w <= 0 || h <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 0, 0
	}
	c = c.valid()
	cols = maxCols
	rows = c.rows(w, h, cols)
	if rows > maxRows {
		rows = maxRows
		cols = max(1, min(maxCols, (2*rows*w*c.H+h*c.W)/(2*h*c.W)))
	}
	return cols, rows
}

// Fit and Fill for DefaultCell.
func Fit(w, h, maxCols, maxRows int) (cols, rows int) {
	return DefaultCell.Fit(w, h, maxCols, maxRows)
}
func Fill(w, h, maxCols, maxRows int) (cols, rows int) {
	return DefaultCell.Fill(w, h, maxCols, maxRows)
}

// HalfBlocks draws im in cols × rows cells: ▀ with the top pixel as the
// foreground and the bottom one as the background.
func HalfBlocks(im image.Image, cols, rows int) []string {
	px := Scale(im, cols, rows*2)
	out := make([]string, rows)
	for r := range rows {
		var b strings.Builder
		for c := range cols {
			t, u := px.RGBAAt(c, 2*r), px.RGBAAt(c, 2*r+1)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", t.R, t.G, t.B, u.R, u.G, u.B)
		}
		b.WriteString("\x1b[39;49m")
		out[r] = b.String()
	}
	return out
}

// Scale shrinks (or grows) im to w × h by averaging the pixels each new one
// covers. Transparency stays (kitty shows the terminal through it); half
// blocks draw it over black.
func Scale(im image.Image, w, h int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	src := im.Bounds()
	sw, sh := src.Dx(), src.Dy()
	if sw == 0 || sh == 0 {
		return out
	}
	for y := range h {
		y0, y1 := src.Min.Y+y*sh/h, src.Min.Y+max((y+1)*sh/h, y*sh/h+1)
		for x := range w {
			x0, x1 := src.Min.X+x*sw/w, src.Min.X+max((x+1)*sw/w, x*sw/w+1)
			var r, g, b, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.RGBAModel.Convert(im.At(sx, sy)).(color.RGBA) // premultiplied: clear pixels weigh nothing
					r, g, b, a, n = r+uint64(c.R), g+uint64(c.G), b+uint64(c.B), a+uint64(c.A), n+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), uint8(a / n)})
		}
	}
	return out
}

// Decode reads a PNG, JPEG or GIF registered with image; it refuses huge
// ones before decoding them.
func Decode(data []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > 40_000_000 {
		return nil, fmt.Errorf("image too large: %d×%d", cfg.Width, cfg.Height)
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	return im, err
}

// PNG encodes im.
func PNG(im image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	return b.Bytes()
}
