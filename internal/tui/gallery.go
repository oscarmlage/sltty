package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

// picSpot is where a picture sits: lines [line, line+rows) and columns
// [x, x+cols) of a rendered list, or of a pane on screen. A click there
// opens it in the viewer.
type picSpot struct {
	line, rows, x, cols int
	msg, file           int // the message, and the picture in its Files
}

// galleryGap is the space between pictures side by side.
const galleryGap = 1

// gallery lays a message's attachments out: pictures side by side while
// they fit width, in as many rows as they need, and each name that shows
// instead of a picture on a line of its own.
func gallery(files []chat.File, width int, a art) ([]string, []picSpot) {
	var lines []string
	var spots, row []picSpot
	var cells [][]string
	x := 0
	flush := func() {
		h := 0
		for _, c := range cells {
			h = max(h, len(c))
		}
		top := len(lines)
		for k := range h {
			var b strings.Builder
			for j, c := range cells {
				if j > 0 {
					b.WriteString(strings.Repeat(" ", galleryGap))
				}
				l := ""
				if k < len(c) {
					l = c[k]
				}
				b.WriteString(l)
				if j < len(cells)-1 {
					b.WriteString(strings.Repeat(" ", max(0, row[j].cols-lipgloss.Width(l))))
				}
			}
			lines = append(lines, b.String())
		}
		for _, s := range row {
			s.line = top
			spots = append(spots, s)
		}
		row, cells, x = nil, nil, 0
	}
	for i, f := range files {
		ls, pic := a.fileLines(f, width)
		if !pic {
			flush()
			lines = append(lines, ls...)
			continue
		}
		w := 0
		for _, l := range ls {
			w = max(w, lipgloss.Width(l))
		}
		if len(row) > 0 && x+galleryGap+w > width {
			flush()
		}
		if len(row) > 0 {
			x += galleryGap
		}
		row = append(row, picSpot{rows: len(ls), x: x, cols: w, file: i})
		cells = append(cells, ls)
		x += w
	}
	flush()
	return lines, spots
}
