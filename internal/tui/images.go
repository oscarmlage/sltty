package tui

import (
	"cmp"
	"context"
	"fmt"
	"image"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/emoji"
	"github.com/oscarmlage/sltty/internal/img"
)

// Images and custom emoji, slk's way (see package img). Pictures are
// fetched from the backend when a conversation or thread shows them, decoded
// off the UI loop and kept in memory only, within imgBudget; with kitty each
// is uploaded once at a fixed size and drawn as placeholder cells. The
// images_toggle key turns them off and on; "images": "off" in the config
// turns them off for good.

const (
	imgBudget       = 64 << 20 // bytes of pictures kept in memory
	defaultImgCols  = 40
	defaultImgRows  = 14
	emojiCols       = 2
	avatarCols      = 4 // slk's: 4 × 2 cells, a square
	avatarRows      = 2
	kittyFirstImage = 0x100 // image ids are the 24-bit foreground color
)

type images struct {
	mode         img.Mode // what the terminal gets: off, half blocks or kitty
	on           bool     // images_toggle
	emoji        bool     // custom emoji as pictures (kitty only)
	cols, rows   int      // the largest picture
	cell         img.Cell // a cell's size in pixels, once the terminal says
	pics         map[string]*picture
	order        []string // fetch order, to drop the oldest past the budget
	used         int
	custom       map[string]bool // the workspace's :emoji:
	customLoaded bool
}

type picture struct {
	loading    bool
	err        error
	im         image.Image // scaled down to what a cell grid can show
	png        []byte      // for kitty
	id         uint32      // kitty image id once uploaded
	cols, rows int         // its size on screen
	half       map[[2]int][]string
	size       int
}

type (
	pictureMsg struct {
		id         string
		im         image.Image
		png        []byte
		cols, rows int      // when the size was not known before
		cell       img.Cell // the cell shape it was fit to
		err        error
	}
	customEmojiMsg struct {
		names []string
		err   error
	}
)

func newImages(o Options) images {
	return images{
		on: true, emoji: o.CustomEmoji,
		cols: cmp.Or(o.ImageWidth, defaultImgCols), rows: cmp.Or(o.ImageHeight, defaultImgRows),
		pics: map[string]*picture{}, custom: map[string]bool{},
	}
}

func (s *images) active() bool { return s.on && s.mode != img.Off }

// toggleImages is images_toggle.
func (m *model) toggleImages() {
	s := &m.imgs
	if s.mode == img.Off {
		m.status = `images are off: "images": "auto" in the config turns them on`
		return
	}
	s.on = !s.on
	m.status = "images off"
	if s.on {
		m.status = "images on (" + s.mode.String() + ")"
	}
}

// imageCmds fetches what the open conversation and thread show and is not
// in memory yet. It runs after every update: cheap, a map lookup a file.
func (m *model) imageCmds() tea.Cmd {
	s := &m.imgs
	if !s.active() {
		return nil
	}
	var cmds []tea.Cmd
	if s.emoji && s.mode == img.Kitty {
		cmds = append(cmds, m.loadCustomEmoji())
	}
	want := func(id string, cols, rows int) {
		if _, ok := s.pics[id]; ok {
			return
		}
		s.pics[id] = &picture{loading: true, cols: cols, rows: rows}
		cmds = append(cmds, m.fetchPicture(id, cols, rows))
	}
	custom := func(name string) {
		if name = strings.Trim(name, ":"); s.emoji && s.mode == img.Kitty && s.custom[name] {
			want("emoji:"+name, emojiCols, 1)
		}
	}
	scan := func(msgs []chat.Message, avatars bool) {
		for _, msg := range msgs {
			if avatars && msg.UserID != "" {
				want("avatar:"+msg.UserID, avatarCols, avatarRows)
			}
			for _, f := range msg.Files {
				if f.IsImage() {
					c, r := s.cell.Fit(f.W, f.H, s.cols, s.rows) // 0 × 0: from the picture
					want(f.ID, c, r)
				}
			}
			for _, n := range emoji.Shortcode.FindAllString(msg.Text, -1) {
				custom(n)
			}
			for _, r := range msg.Reactions {
				custom(r.Emoji)
			}
		}
	}
	if c := m.current(); c != nil && m.view == sideConv {
		scan(m.history[c.ID], true)
	}
	if m.thread != nil {
		scan(m.thread.msgs, true)
	}
	if m.view == sideThreads {
		for _, t := range m.threads {
			scan(append([]chat.Message{t.Root}, t.Latest...), true)
		}
	}
	if m.view == sideActivity {
		for _, a := range m.activity {
			if a.Kind == chat.ActReaction {
				custom(a.Emoji)
			}
			if a.ByID != "" {
				want("avatar:"+a.ByID, avatarCols, avatarRows)
			}
		}
	}
	for _, sec := range m.sections() {
		if _, std := emoji.Glyph(sec.emoji); sec.emoji != "" && !std {
			custom(sec.emoji)
		}
	}
	if p := m.picker; p != nil && p.kind == pickReact {
		for _, it := range p.items[p.first:min(len(p.items), p.first+pickerRows)] {
			if strings.HasPrefix(it.emoji, ":") {
				custom(it.emoji)
			}
		}
	}
	return tea.Batch(cmds...)
}

// loadCustomEmoji fetches the names of the workspace's emoji, once: for
// their pictures and the reaction picker.
func (m *model) loadCustomEmoji() tea.Cmd {
	if m.imgs.customLoaded {
		return nil
	}
	m.imgs.customLoaded = true
	return m.call(func(ctx context.Context) tea.Msg {
		n, err := m.backend.CustomEmoji(ctx)
		return customEmojiMsg{n, err}
	})
}

// fetchPicture gets, decodes and shrinks a picture off the UI loop: a few
// pixels a cell are all a terminal shows. A picture whose size the message
// did not say (0 × 0) takes it from the decoded image.
func (m *model) fetchPicture(id string, cols, rows int) tea.Cmd {
	maxCols, maxRows, cell := m.imgs.cols, m.imgs.rows, m.imgs.cell
	return m.call(func(ctx context.Context) tea.Msg {
		data, err := m.backend.File(ctx, id)
		if err != nil {
			return pictureMsg{id: id, err: err, cell: cell}
		}
		return shrink(id, data, cell, false, cols, rows, maxCols, maxRows, 12)
	})
}

// shrink decodes a picture for cols × rows cells of a cell's shape, px
// pixels a cell wide: a picture whose size was not known (0 × 0) gets the
// cells Fit says for maxCols × maxRows, or Fill's when fill is set.
func shrink(id string, data []byte, cell img.Cell, fill bool, cols, rows, maxCols, maxRows, px int) pictureMsg {
	im, err := img.Decode(data)
	if err != nil {
		return pictureMsg{id: id, err: err, cell: cell}
	}
	b := im.Bounds()
	if cols == 0 || rows == 0 {
		fit := cell.Fit
		if fill {
			fit = cell.Fill
		}
		if cols, rows = fit(b.Dx(), b.Dy(), maxCols, maxRows); cols == 0 {
			return pictureMsg{id: id, err: fmt.Errorf("empty picture"), cell: cell}
		}
	}
	c := cell
	if c.W <= 0 || c.H <= 0 {
		c = img.DefaultCell
	}
	if w, h := cols*px, rows*px*c.H/c.W; b.Dx() > w || b.Dy() > h {
		// The aspect stays: the terminal fits it to the cells.
		f := min(float64(w)/float64(b.Dx()), float64(h)/float64(b.Dy()))
		im = img.Scale(im, max(1, int(float64(b.Dx())*f)), max(1, int(float64(b.Dy())*f)))
	}
	return pictureMsg{id: id, im: im, png: img.PNG(im), cols: cols, rows: rows, cell: cell}
}

// cellSizeQuery asks the terminal for its cell size in pixels: XTWINOPS
// 16, answered with CSI 6 ; height ; width t.
const cellSizeQuery = "\x1b[16t"

// setCell takes the terminal's cell size (CSI 16 t): the attachments fit
// to another shape go and are fetched again at this one. Emoji and avatars
// keep their cells.
func (m *model) setCell(w, h int) tea.Cmd {
	s := &m.imgs
	c := img.Cell{W: w, H: h}
	if w <= 0 || h <= 0 || c == s.cell {
		return nil
	}
	s.cell = c
	var out strings.Builder
	keep := s.order[:0]
	for _, id := range s.order {
		if p := s.pics[id]; p != nil && !strings.Contains(id, ":") {
			s.used -= p.size
			if p.id != 0 {
				out.WriteString(img.Delete(p.id))
			}
			delete(s.pics, id)
			continue
		}
		keep = append(keep, id)
	}
	s.order = keep
	for id := range s.pics {
		if !strings.Contains(id, ":") {
			delete(s.pics, id) // loading: what comes is for the old shape
		}
	}
	cmds := []tea.Cmd{m.imageCmds()}
	if m.viewer != nil {
		cmds = append(cmds, m.loadViewer())
	}
	if out.Len() > 0 {
		cmds = append(cmds, tea.Raw(out.String()))
	}
	return tea.Batch(cmds...)
}

// gotPicture keeps a fetched picture, uploads it to kitty and drops the
// oldest past the budget.
func (m *model) gotPicture(msg pictureMsg) tea.Cmd {
	s := &m.imgs
	p := s.pics[msg.id]
	if p == nil || msg.cell != s.cell && !strings.Contains(msg.id, ":") {
		return nil // dropped, or fit to the cells before the terminal said
	}
	p.loading, p.err = false, msg.err
	if msg.err != nil {
		return nil
	}
	if msg.cols > 0 && msg.rows > 0 {
		p.cols, p.rows = msg.cols, msg.rows
	}
	b := msg.im.Bounds()
	p.im, p.png, p.size = msg.im, msg.png, b.Dx()*b.Dy()*4+len(msg.png)
	s.used += p.size
	s.order = append(s.order, msg.id)
	var out strings.Builder
	for s.used > imgBudget && len(s.order) > 1 {
		oldID := s.order[0]
		s.order = s.order[1:]
		if old := s.pics[oldID]; old != nil {
			if old.id != 0 {
				out.WriteString(img.Delete(old.id))
			}
			s.used -= old.size
		}
		delete(s.pics, oldID) // fetched again if it shows again
	}
	if s.mode == img.Kitty {
		p.id = newImageID()
		out.WriteString(img.Upload(p.id, p.png, p.cols, p.rows))
		p.png = nil // the terminal has it now
	}
	if out.Len() == 0 {
		return nil
	}
	return tea.Raw(out.String())
}

// forgetImages is the escape sequence that frees every uploaded picture,
// on the way out.
func (m *model) forgetImages() string {
	var b strings.Builder
	for _, p := range m.imgs.pics {
		if p.id != 0 {
			b.WriteString(img.Delete(p.id))
		}
	}
	return b.String()
}

// fileLines draws an attachment at most width cells wide: the picture, as
// slk does, or its name while pictures are off, it loads or does not fit.
func (m *model) fileLines(f chat.File, width int) ([]string, bool) {
	icon := "📎 "
	if f.IsImage() {
		icon = "🖼  "
	}
	label := linkStyle.Render(icon + f.Name)
	if f.W > 0 {
		label += dimStyle.Render(fmt.Sprintf("  %d×%d", f.W, f.H))
	}
	s := &m.imgs
	p := s.pics[f.ID]
	switch {
	case !f.IsImage() || !s.active():
		return []string{label}, false
	case p == nil || p.loading:
		return []string{label + dimStyle.Render("  loading…")}, false
	case p.err != nil:
		return []string{label + dimStyle.Render("  (no preview)")}, false
	}
	if p.cols > width && s.mode == img.Kitty {
		return []string{label}, false // uploaded at its size: it cannot shrink
	}
	if pic := m.pictureLines(p, min(p.cols, width), p.rows); pic != nil {
		return pic, true
	}
	return []string{label}, false
}

// pictureLines draws a fetched picture in cols × rows cells: kitty's
// placeholders, or half blocks at any size.
func (m *model) pictureLines(p *picture, cols, rows int) []string {
	switch m.imgs.mode {
	case img.Kitty:
		if p.id != 0 {
			return img.Cells(p.id, p.cols, p.rows)
		}
	case img.HalfBlock:
		if p.im == nil {
			return nil
		}
		b := p.im.Bounds()
		if cols < p.cols { // narrower than it was fit to: fit again
			cols, rows = m.imgs.cell.Fit(b.Dx(), b.Dy(), cols, rows)
		}
		if p.half == nil {
			p.half = map[[2]int][]string{}
		}
		k := [2]int{cols, rows}
		if p.half[k] == nil {
			p.half[k] = img.HalfBlocks(p.im, cols, rows)
		}
		return p.half[k]
	}
	return nil
}

// avatars tells whether messages carry their author's picture: with
// pictures on, in a conversation (a thread is too narrow, as in slk).
func (m *model) avatars() bool { return m.imgs.active() }

// avatarLines is an author's picture, avatarRows lines of avatarCols
// cells; blank until it comes or when there is none.
func (m *model) avatarLines(userID string) []string {
	if p := m.imgs.pics["avatar:"+userID]; p != nil && !p.loading && p.err == nil {
		if pic := m.pictureLines(p, avatarCols, avatarRows); len(pic) == avatarRows {
			return pic
		}
	}
	blank := strings.Repeat(" ", avatarCols)
	return []string{blank, blank}
}

// emojiCells is a custom emoji as a picture, when there is one to show.
func (m *model) emojiCells(name string) (string, bool) {
	s := &m.imgs
	if !s.active() || !s.emoji || s.mode != img.Kitty {
		return "", false
	}
	p := s.pics["emoji:"+name]
	if p == nil || p.id == 0 {
		return "", false
	}
	return img.Cells(p.id, emojiCols, 1)[0], true
}

// emojiText is a Slack emoji name (a section's, "tomato") as it shows: its
// character, a custom one's picture, or nothing.
func (m *model) emojiText(name string) string {
	name = strings.Trim(name, ":")
	if name == "" {
		return ""
	}
	if g, ok := emoji.Glyph(name); ok {
		return g
	}
	if cells, ok := m.emojiCells(name); ok {
		return cells
	}
	return ""
}

// richText styles one line of message text: custom emoji as pictures, the
// words of the last search marked. Standard emoji are characters already
// (emoji.Expand, before wrapping).
func (m *model) richText(line string, words []string) string {
	line = slackMrkdwn(expandTabs(line))
	if !strings.Contains(line, ":") {
		return highlight(line, words)
	}
	var b strings.Builder
	last := 0
	for _, loc := range emoji.Shortcode.FindAllStringIndex(line, -1) {
		cells, ok := m.emojiCells(line[loc[0]+1 : loc[1]-1])
		if !ok {
			continue
		}
		b.WriteString(highlight(line[last:loc[0]], words))
		b.WriteString(cells)
		last = loc[1]
	}
	b.WriteString(highlight(line[last:], words))
	return b.String()
}

// reactionEmoji is a reaction's emoji: a character, or a picture for a
// custom one.
func (m *model) reactionEmoji(e string) string {
	if strings.HasPrefix(e, ":") && strings.HasSuffix(e, ":") {
		if g := m.emojiText(e); g != "" {
			return g
		}
	}
	return e
}

// lastImageID numbers kitty images for the whole program: every account
// shares the one terminal, so their ids must never collide.
var lastImageID atomic.Uint32

func newImageID() uint32 {
	lastImageID.CompareAndSwap(0, kittyFirstImage-1)
	return lastImageID.Add(1)
}
