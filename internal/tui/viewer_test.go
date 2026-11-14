package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/img"
)

// stubArt draws pictures as letters: f.ID repeated, cells[f.ID] high.
type stubArt map[string][2]int

func (s stubArt) fileLines(f chat.File, width int) ([]string, bool) {
	size, ok := s[f.ID]
	if !ok || size[0] > width {
		return []string{"📎 " + f.Name}, false
	}
	pic := make([]string, size[1])
	for i := range pic {
		pic[i] = strings.Repeat(f.ID, size[0])
	}
	return pic, true
}
func (stubArt) richText(line string, _ []string) string { return line }
func (stubArt) reactionEmoji(e string) string           { return e }
func (stubArt) avatars() bool                           { return true }
func (stubArt) avatarLines(id string) []string          { return []string{"[" + id + "]", "[" + id + "]"} }

func TestGalleryPutsPicturesSideBySide(t *testing.T) {
	art := stubArt{"a": {10, 3}, "b": {10, 2}, "d": {10, 3}}
	files := []chat.File{{ID: "a"}, {ID: "b"}, {ID: "c", Name: "notes.pdf"}, {ID: "d"}}
	lines, spots := gallery(files, 25, art)
	want := []string{
		"aaaaaaaaaa bbbbbbbbbb",
		"aaaaaaaaaa bbbbbbbbbb",
		"aaaaaaaaaa ",
		"📎 notes.pdf",
		"dddddddddd", "dddddddddd", "dddddddddd",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("laid out\n%s", strings.Join(lines, "\n"))
	}
	wantSpots := []picSpot{
		{line: 0, rows: 3, x: 0, cols: 10, file: 0},
		{line: 0, rows: 2, x: 11, cols: 10, file: 1},
		{line: 4, rows: 3, x: 0, cols: 10, file: 3},
	}
	if fmt.Sprint(spots) != fmt.Sprint(wantSpots) {
		t.Fatalf("spots %+v", spots)
	}
	// Narrow: one under the other.
	lines, spots = gallery(files[:2], 15, art)
	if len(lines) != 5 || len(spots) != 2 || spots[1].line != 3 || spots[1].x != 0 {
		t.Fatalf("narrow: %q %+v", lines, spots)
	}
}

func TestAvatarOnEveryMessage(t *testing.T) {
	at := time.Date(2025, 1, 2, 10, 0, 0, 0, time.Local)
	msgs := []chat.Message{
		{User: "ana", UserID: "U1", Text: "uno", At: at},
		{User: "ana", UserID: "U1", Text: "dos", At: at.Add(time.Minute), Files: []chat.File{{ID: "p"}}},
	}
	lines, blocks := renderMsgs(msgs, 60, false, "me", at, "", nil, stubArt{"p": {6, 2}})
	for i, b := range blocks {
		for k := b.from; k < b.from+2; k++ {
			if !strings.HasPrefix(lines[k], " [U1] ") {
				t.Fatalf("message %d line %d without its avatar: %q", i, k, lines[k])
			}
		}
	}
	if strings.Count(strings.Join(lines, "\n"), "ana") != 1 {
		t.Fatal("the name should still head only the run")
	}
	p := blocks[1].pics
	if len(p) != 1 || lines[p[0].line][p[0].x:p[0].x+p[0].cols] != "pppppp" {
		t.Fatalf("picture spot %+v in %q", p, lines)
	}
}

// raws is what cmd writes to the terminal as it is.
func raws(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	switch msg := cmd().(type) {
	case tea.RawMsg:
		return fmt.Sprint(msg.Msg)
	case tea.BatchMsg:
		var b strings.Builder
		for _, c := range msg {
			b.WriteString(raws(c))
		}
		return b.String()
	}
	return ""
}

// twoPictures adds the diagram to the dashboard's message and fetches it.
func twoPictures(t *testing.T, m *model) int {
	t.Helper()
	c := m.current()
	for i, msg := range m.history[c.ID] {
		if len(msg.Files) == 1 && msg.Files[0].ID == "F1" {
			m.history[c.ID][i].Files = append(msg.Files, chat.File{ID: "F2", Name: "arquitectura.png", Mime: "image/png", W: 600, H: 400})
			fetch(m, m.imageCmds())
			m.focus, m.sel[c.ID] = paneChat, i
			return i
		}
	}
	t.Fatal("no message with the dashboard")
	return 0
}

func TestClickOpensThePictureBig(t *testing.T) {
	m := withImages(t, img.Kitty)
	twoPictures(t, m)
	small, other := m.imgs.pics["F1"], m.imgs.pics["F2"]
	if small == nil || small.id == 0 || other == nil || other.id == 0 {
		t.Fatal("pictures not uploaded")
	}
	out := m.render()
	var hits []picHit
	for _, h := range m.layout.pics {
		if h.pane == paneChat {
			hits = append(hits, h)
		}
	}
	if len(hits) != 2 || hits[0].line != hits[1].line || hits[1].x != hits[0].x+hits[0].cols+galleryGap {
		t.Fatalf("not side by side: %+v", hits)
	}
	rows := strings.Split(out, "\n")
	row := rows[hits[0].line+m.layout.top]
	if !strings.Contains(row, kittyColor(small.id)) || !strings.Contains(row, kittyColor(other.id)) {
		t.Fatal("the two pictures are not on the same lines")
	}
	if c := ansi.Cut(row, hits[0].x, hits[0].x+1); !strings.Contains(c, string(img.Placeholder)) {
		t.Fatalf("the spot is off the picture: %q", ansi.Strip(c))
	}
	if c := ansi.Cut(row, hits[0].x-1, hits[0].x); strings.Contains(c, string(img.Placeholder)) {
		t.Fatal("the spot starts inside the picture")
	}

	_, cmd := m.Update(tea.MouseClickMsg{X: hits[1].x + 3, Y: hits[1].line + m.layout.top + 2, Button: tea.MouseLeft})
	fetch(m, cmd)
	v := m.viewer
	if v == nil || v.at != 1 || v.pic == nil || v.pic.id == 0 {
		t.Fatalf("no viewer on the diagram: %+v", v)
	}
	if v.pic.cols <= other.cols || v.pic.rows <= other.rows {
		t.Fatalf("not bigger: %d×%d", v.pic.cols, v.pic.rows)
	}
	out = m.render()
	if !strings.Contains(out, kittyColor(v.pic.id)) || !strings.Contains(ansi.Strip(out), "arquitectura.png") || !strings.Contains(ansi.Strip(out), "2/2") {
		t.Fatalf("viewer:\n%s", ansi.Strip(out))
	}
	lines := strings.Split(out, "\n")
	if len(lines) != m.height {
		t.Fatalf("%d lines, want %d", len(lines), m.height)
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w != m.width {
			t.Fatalf("a line %d wide, want %d", w, m.width)
		}
	}
	id := v.pic.id
	_, cmd = m.Update(named["esc"])
	if m.viewer != nil || !strings.Contains(raws(cmd), fmt.Sprintf("i=%d,", id)) {
		t.Fatal("esc did not close the viewer and free its picture")
	}
	if out := m.render(); strings.Contains(out, kittyColor(id)) || !strings.Contains(out, kittyColor(small.id)) {
		t.Fatal("not back to the conversation")
	}
}

func TestViewKeyGoesThroughPictures(t *testing.T) {
	m := withImages(t, img.HalfBlock)
	twoPictures(t, m)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	fetch(m, cmd)
	if v := m.viewer; v == nil || v.at != 0 || v.pic == nil || v.pic.im == nil {
		t.Fatalf("v did not show the first picture: %+v", v)
	}
	if s := screen(m); !strings.Contains(s, "runners-dashboard.png") || !strings.Contains(s, "1/2") || !strings.Contains(m.render(), "▀") {
		t.Fatalf("viewer:\n%s", s)
	}
	for _, want := range []string{"arquitectura.png", "runners-dashboard.png"} {
		_, cmd = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
		fetch(m, cmd)
		if !strings.Contains(screen(m), want) {
			t.Fatalf("l did not go to %s", want)
		}
	}
	press(m, "esc")
	if m.viewer != nil || m.focus != paneChat {
		t.Fatal("esc should only close the viewer")
	}
	m.imgs.on = false
	press(m, "v")
	if m.viewer != nil {
		t.Fatal("a viewer with pictures off")
	}
}
