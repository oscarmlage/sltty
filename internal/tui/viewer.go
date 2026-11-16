package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/img"
)

// The viewer is slk's image preview: a message's picture as big as the
// window, from a click on it or view_picture. The original is fetched for
// it and dropped on the way out; h and l go through the message's
// pictures, esc goes back.

// viewerPx is how many pixels a viewer cell gets across (twice that down):
// enough for a sharp picture on a dense screen.
const viewerPx = 20

type viewer struct {
	files []chat.File // the message's pictures
	at    int
	pic   *picture // the one showing
	want  string   // what pic is: a load for anything else is stale
}

type bigPictureMsg struct {
	want string
	pictureMsg
}

// openViewer shows picture file (an index into msg.Files) of msg.
func (m *model) openViewer(msg *chat.Message, file int) tea.Cmd {
	if msg == nil {
		return nil
	}
	if !m.imgs.active() {
		m.status = "pictures are off: images_toggle turns them on"
		return nil
	}
	v := &viewer{}
	for i, f := range msg.Files {
		if f.IsImage() {
			if i == file {
				v.at = len(v.files)
			}
			v.files = append(v.files, f)
		}
	}
	if len(v.files) == 0 {
		m.status = "no picture in this message"
		return nil
	}
	m.viewer = v
	return m.loadViewer()
}

// viewerBox is the picture's room: the window less the caption and the
// hints, within what a placeholder can number.
func (m *model) viewerBox() (cols, rows int) {
	return max(1, min(m.width-2, img.MaxCells)), max(1, min(m.height-2, img.MaxCells))
}

// loadViewer fetches the picture showing for the window's size, unless it
// has it already.
func (m *model) loadViewer() tea.Cmd {
	v := m.viewer
	f := v.files[v.at]
	maxCols, maxRows := m.viewerBox()
	cell := m.imgs.cell
	want := fmt.Sprintf("%s@%dx%d/%v", f.ID, maxCols, maxRows, cell)
	if v.want == want {
		return nil
	}
	drop := m.dropViewerPicture()
	v.want, v.pic = want, &picture{loading: true}
	cols, rows := cell.Fill(f.W, f.H, maxCols, maxRows) // 0 × 0: from the picture
	load := m.call(func(ctx context.Context) tea.Msg {
		var p pictureMsg
		// The original, or the thumbnail when it is too big or in a
		// format Go does not read (HEIC).
		for _, id := range []string{"full:" + f.ID, f.ID} {
			data, err := m.backend.File(ctx, id)
			if err != nil {
				p = pictureMsg{id: f.ID, err: err}
				continue
			}
			if p = shrink(f.ID, data, cell, true, cols, rows, maxCols, maxRows, viewerPx); p.err == nil {
				break
			}
		}
		return bigPictureMsg{want, p}
	})
	return tea.Batch(drop, load)
}

// gotBigPicture shows a fetched picture in the viewer still waiting for it.
func (m *model) gotBigPicture(msg bigPictureMsg) tea.Cmd {
	v := m.viewer
	if v == nil || v.want != msg.want {
		return nil
	}
	p := &picture{err: msg.err}
	v.pic = p
	if msg.err != nil {
		return nil
	}
	p.im, p.cols, p.rows = msg.im, msg.cols, msg.rows
	if m.imgs.mode != img.Kitty {
		return nil
	}
	p.id = newImageID()
	return tea.Raw(img.Upload(p.id, msg.png, p.cols, p.rows))
}

// dropViewerPicture frees the viewer's picture in the terminal.
func (m *model) dropViewerPicture() tea.Cmd {
	v := m.viewer
	if v == nil || v.pic == nil || v.pic.id == 0 {
		return nil
	}
	id := v.pic.id
	v.pic = nil
	return tea.Raw(img.Delete(id))
}

func (m *model) closeViewer() tea.Cmd {
	cmd := m.dropViewerPicture()
	m.viewer = nil
	return cmd
}

// viewerStep goes to the message's next (1) or previous (-1) picture.
func (m *model) viewerStep(d int) tea.Cmd {
	v := m.viewer
	n := len(v.files)
	if n < 2 {
		return nil
	}
	v.at = (v.at + d + n) % n
	return m.loadViewer()
}

// viewerKey: esc and the other ways back close it, left and right (or up
// and down) go through the pictures.
func (m *model) viewerKey(k string) tea.Cmd {
	switch a := m.km.lookup[scNormal][k]; a {
	case actFocusLeft, actUp, actPrevMatch:
		return m.viewerStep(-1)
	case actFocusRight, actDown, actNextMatch:
		return m.viewerStep(1)
	case actBack, actBackOrQuit, actView, actOpen:
		return m.closeViewer()
	case actQuit:
		cmd := m.closeViewer()
		m.quitting = true
		return cmd
	}
	return nil
}

// viewerMouse: a click closes it, the wheel goes through the pictures.
func (m *model) viewerMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		return m.closeViewer()
	case tea.MouseWheelMsg:
		switch msg.Mouse().Button {
		case tea.MouseWheelDown:
			return m.viewerStep(1)
		case tea.MouseWheelUp:
			return m.viewerStep(-1)
		}
	}
	return nil
}

// viewerScreen draws the window: the caption, the picture in the middle,
// the keys.
func (m *model) viewerScreen() []string {
	v := m.viewer
	f := v.files[v.at]
	caption := f.Name
	if f.W > 0 {
		caption += fmt.Sprintf("  ·  %d×%d", f.W, f.H)
	}
	if len(v.files) > 1 {
		caption += fmt.Sprintf("  ·  %d/%d", v.at+1, len(v.files))
	}
	out := []string{paint(" "+headerStyle.Render(caption), m.width, paneBg)}
	var body []string
	switch p := v.pic; {
	case p == nil || p.loading:
		body = []string{dimStyle.Render("loading…")}
	case p.err != nil:
		body = []string{errStyle.Render("no picture: " + p.err.Error())}
	default:
		body = m.pictureLines(p, p.cols, p.rows)
	}
	rows := max(1, m.height-2)
	top := max(0, (rows-len(body))/2)
	for i := range rows {
		l := ""
		if k := i - top; k >= 0 && k < len(body) {
			l = strings.Repeat(" ", max(0, (m.width-lipgloss.Width(body[k]))/2)) + body[k]
		}
		out = append(out, paint(l, m.width, paneBg))
	}
	hs := [][2]string{{"esc", "back"}}
	if len(v.files) > 1 {
		hs = append([][2]string{{"h l", "previous / next"}}, hs...)
	}
	out = append(out, paint(" "+hints(hs), m.width, paneBg))
	return out[:min(len(out), m.height)]
}
