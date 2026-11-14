package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/img"
)

// withImages runs the demo in an image mode, fetching what it shows.
func withImages(t *testing.T, mode img.Mode) *model {
	m := newTest(t)
	m.goTo(t, "tools-team")
	m.imgs.mode, m.imgs.emoji = mode, true // now: fetched below, not by run()
	for range 3 {                          // emoji list, then the pictures it lets through
		cmd := m.imageCmds()
		if cmd == nil {
			continue
		}
		fetch(m, cmd)
	}
	return m
}

// fetch runs cmd and the batches in it to the end, however slow (-race):
// no quick() deadline for pictures.
func fetch(m *model, cmd tea.Cmd) {
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			if c != nil {
				fetch(m, c)
			}
		}
	default:
		run(m, msg)
	}
}

func TestKittyImagesAndEmoji(t *testing.T) {
	m := withImages(t, img.Kitty)
	p := m.imgs.pics["F1"]
	if p == nil || p.id == 0 || p.png != nil {
		t.Fatalf("dashboard not uploaded: %+v", p)
	}
	if e := m.imgs.pics["emoji:sltty"]; e == nil || e.id == 0 {
		t.Fatal("custom emoji not fetched")
	}
	press(m, "l")
	for range 6 {
		press(m, "k")
	}
	out := m.render()
	if !strings.Contains(out, kittyColor(p.id)) {
		t.Fatal("no placeholders for the image")
	}
	if av := m.imgs.pics["avatar:"+m.history["C2"][0].UserID]; av == nil || av.id == 0 || !strings.Contains(out, kittyColor(av.id)) {
		t.Fatalf("no avatar: %+v", av)
	}
	if strings.Contains(ansiStrip(out), ":sltty:") || strings.Contains(ansiStrip(out), ":shipit:") {
		t.Fatal("custom emoji left as text")
	}
	for _, l := range strings.Split(out, "\n") {
		if w := lipglossWidth(l); w != m.width {
			t.Fatalf("a line %d wide, want %d", w, m.width)
		}
	}
	press(m, "?") // a dialog: the backdrop must not dim the image ids
	if strings.Contains(m.render(), string(img.Placeholder)) {
		t.Fatal("placeholders under the backdrop")
	}
	press(m, "q", "I")
	if m.imgs.on || strings.Contains(m.render(), string(img.Placeholder)) || !strings.Contains(ansiStrip(m.render()), ":sltty:") {
		t.Fatal("I did not turn images off")
	}
	press(m, "I")
	if !m.imgs.on {
		t.Fatal("I did not turn them back on")
	}
	m.quitting = true
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil || isQuit(cmd) { // a sequence: free the images, then quit
		t.Fatal("quit did not free the images first")
	}
}

func TestPicturesFitTheTerminalsCells(t *testing.T) {
	m := withImages(t, img.Kitty)
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	if !strings.Contains(raws(cmd), cellSizeQuery) {
		t.Fatal("the cell size not asked")
	}
	before, avatar := m.imgs.pics["F1"], m.imgs.pics["avatar:"+m.history["C2"][0].UserID]
	if before == nil || before.rows != 11 || avatar == nil {
		t.Fatalf("dashboard %+v", before)
	}
	// 7 × 17 pixel cells are taller than 1 × 2: the 640 × 360 dashboard
	// takes 9 rows, not 11 with a blank under it.
	_, cmd = m.Update(uv.CellSizeEvent{Width: 7, Height: 17})
	fetch(m, cmd)
	after := m.imgs.pics["F1"]
	if after == nil || after.loading || after.rows != 9 || after.cols != 40 || after.id == before.id {
		t.Fatalf("not fit again: %+v", after)
	}
	if m.imgs.pics["avatar:"+m.history["C2"][0].UserID] != avatar {
		t.Fatal("avatars keep their cells")
	}
	if _, cmd = m.Update(uv.CellSizeEvent{Width: 7, Height: 17}); cmd != nil {
		t.Fatal("the same cells fetched again")
	}
}

func TestCustomEmojiReactionInActivity(t *testing.T) {
	m := withImages(t, img.Kitty)
	run(m, m.openActivityView()())
	i := slices.IndexFunc(m.activity, func(a chat.Activity) bool { return a.Kind == chat.ActReaction })
	if i < 0 {
		t.Fatal("no reaction in the demo's activity")
	}
	m.activity[i].Emoji = ":party-parrot:"
	fetch(m, m.imageCmds())
	p := m.imgs.pics["emoji:party-parrot"]
	if p == nil || p.id == 0 {
		t.Fatal("the reaction's custom emoji not fetched")
	}
	if out := m.render(); !strings.Contains(out, kittyColor(p.id)) || strings.Contains(ansiStrip(out), ":party-parrot:") {
		t.Fatalf("the reaction left as text:\n%s", ansiStrip(out))
	}
}

func TestReactWithAnyEmoji(t *testing.T) {
	m := newTest(t) // no pictures: the names alone
	m.goTo(t, "tools-team")
	press(m, "l", "r")
	if m.picker == nil || len(m.picker.items) != len(reactions) {
		t.Fatal("r does not open with the usual few")
	}
	typeText(m, "sltt")
	if len(m.picker.items) == 0 || m.picker.items[0].emoji != ":sltty:" {
		t.Fatalf("the workspace's emoji not offered: %+v", m.picker.items)
	}
	press(m, "esc", "esc", "r") // the list, then closed
	typeText(m, "avocad")       // none of the usual: one of Slack's
	if len(m.picker.items) == 0 || m.picker.items[0].emoji != "🥑" {
		t.Fatalf("Slack's emoji not offered: %+v", m.picker.items)
	}
	press(m, "esc", "esc", "r")
	typeText(m, "sltt")
	press(m, "enter")
	rs := m.selectedMsg().Reactions
	if len(rs) == 0 || rs[len(rs)-1].Emoji != ":sltty:" || rs[len(rs)-1].Count != 1 || !rs[len(rs)-1].Mine {
		t.Fatalf("reactions %+v", rs)
	}
}

func TestReactionPickerShowsCustomEmoji(t *testing.T) {
	m := withImages(t, img.Kitty)
	press(m, "l", "r")
	typeText(m, "party-p")
	if len(m.picker.items) == 0 || m.picker.items[0].emoji != ":party-parrot:" {
		t.Fatalf("items %+v", m.picker.items)
	}
	if cmd := m.imageCmds(); cmd != nil {
		fetch(m, cmd)
	}
	p := m.imgs.pics["emoji:party-parrot"]
	if p == nil || p.id == 0 {
		t.Fatal("the picker's custom emoji not fetched")
	}
	// The highlighted row too: the selection's style must not eat its id.
	out := m.render()
	i := slices.IndexFunc(strings.Split(out, "\n"), func(l string) bool {
		return strings.Contains(ansiStrip(l), "▌") && strings.Contains(ansiStrip(l), "party-parrot")
	})
	if i < 0 || !strings.Contains(strings.Split(out, "\n")[i], kittyColor(p.id)) {
		t.Fatalf("no picture on the picker's row:\n%s", ansiStrip(out))
	}
	if w := lipglossWidth(strings.Split(out, "\n")[i]); w != m.width {
		t.Fatalf("the picker's row is %d wide", w)
	}
}

func TestHalfBlockImages(t *testing.T) {
	m := withImages(t, img.HalfBlock)
	if m.imgs.pics["emoji:sltty"] != nil {
		t.Fatal("emoji pictures without kitty")
	}
	press(m, "l")
	for range 6 {
		press(m, "k")
	}
	out := m.render()
	if !strings.Contains(out, "▀") || strings.Contains(out, string(img.Placeholder)) {
		t.Fatal("no half blocks")
	}
	if !strings.Contains(ansiStrip(out), ":sltty:") {
		t.Fatal("custom emoji should stay text without kitty")
	}
}

func TestImagesOffInConfig(t *testing.T) {
	m := withImages(t, img.Off)
	if len(m.imgs.pics) != 0 {
		t.Fatal("fetched pictures with images off")
	}
	press(m, "I")
	if m.imgs.on && !strings.Contains(ansiStrip(m.render()), "images are off") {
		t.Fatal("I with images off in the config said nothing")
	}
}

// kittyColor is what tells kitty which image placeholder cells show.
func kittyColor(id uint32) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", byte(id>>16), byte(id>>8), byte(id))
}

func ansiStrip(s string) string  { return ansi.Strip(s) }
func lipglossWidth(s string) int { return lipgloss.Width(s) }
