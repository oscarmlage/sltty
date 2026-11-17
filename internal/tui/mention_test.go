package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMentionWhileWriting(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "general") // ana, bob and carol write there
	press(m, "i")
	typeText(m, "hola @an")
	if m.suggest == nil || len(m.suggest.items) == 0 || m.suggest.items[0].Name != "ana" {
		t.Fatalf("suggest: %+v", m.suggest)
	}
	if s := screen(m); !strings.Contains(s, "@ana") || !strings.Contains(s, "tab pick") {
		t.Fatalf("no list on screen:\n%s", s)
	}
	press(m, "tab")
	if m.chatIn.Value() != "hola @ana " || m.suggest != nil {
		t.Fatalf("picked: %q", m.chatIn.Value())
	}
	typeText(m, "qué tal, @anabel no")
	press(m, "enter")
	h := m.history["C1"]
	if got := h[len(h)-1].Text; got != "hola <@ana> qué tal, @anabel no" {
		t.Fatalf("sent %q", got)
	}
	if len(m.mentions["C1"]) != 0 {
		t.Fatal("the picked mentions outlived the message")
	}
}

func TestMentionAsksTheService(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "general")
	press(m, "i")
	typeText(m, "@fr") // frank never wrote here
	if len(m.suggest.items) != 0 {
		t.Fatalf("frank is no one here yet: %+v", m.suggest.items)
	}
	_, cmd := m.Update(suggestTick{m.suggest.key, "C1", "fr"})
	run(m, cmd())
	if len(m.suggest.items) != 1 || m.suggest.items[0].Full != "Frank Ortega" {
		t.Fatalf("the directory: %+v", m.suggest.items)
	}
	// esc closes the list, not INSERT; a space ends the @word.
	press(m, "esc")
	if m.suggest != nil || m.mode != insert {
		t.Fatalf("esc: suggest %v, mode %v", m.suggest != nil, m.mode)
	}
	typeText(m, " ")
	if m.suggest != nil {
		t.Fatal("no @word, no list")
	}
}

func TestMentionsMoveAndEdit(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "general")
	press(m, "i")
	typeText(m, "@")
	n := len(m.suggest.items)
	if n < 2 {
		t.Fatalf("an empty @ lists the conversation's people: %d", n)
	}
	run(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	press(m, "up", "up")
	if m.suggest.sel != n-1 {
		t.Fatalf("ctrl+n then up up wraps: %d of %d", m.suggest.sel, n)
	}
	press(m, "esc", "esc")
	// Editing a message with a mention reads @name and saves <@ID>.
	m.chatIn.Reset()
	press(m, "i")
	typeText(m, "ping @an")
	press(m, "tab", "enter", "esc")
	h := m.history["C1"]
	if h[len(h)-1].Text != "ping <@ana>" {
		t.Fatalf("sent %q", h[len(h)-1].Text)
	}
	press(m, "e")
	if m.chatIn.Value() != "ping @ana" {
		t.Fatalf("edit shows %q", m.chatIn.Value())
	}
	typeText(m, "!") // editing is writing already
	press(m, "enter")
	if got := m.history["C1"][len(h)-1].Text; got != "ping <@ana>!" {
		t.Fatalf("saved %q", got)
	}
}

func TestReplaceMentionWholeWords(t *testing.T) {
	for in, want := range map[string]string{
		"@ana":          "<@A>",
		"@ana, hi":      "<@A>, hi",
		"@anabel":       "@anabel",
		"@ana.lopez":    "@ana.lopez",
		"@ana y @ana":   "<@A> y <@A>",
		"mail a@ana.es": "mail a@ana.es",
	} {
		if got := replaceMention(in, "@ana", "<@A>"); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
