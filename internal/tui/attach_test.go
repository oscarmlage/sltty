package tui

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPastedPath(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "my shot.png")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{f, " " + f + "\n", "'" + f + "'", `"` + f + `"`, strings.ReplaceAll(f, " ", `\ `), "file://" + strings.ReplaceAll(f, " ", "%20")} {
		if got, ok := pastedPath(in); !ok || got != f {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"hello", "my shot.png", dir, f + "\nsecond line", filepath.Join(dir, "missing.png")} {
		if got, ok := pastedPath(in); ok {
			t.Errorf("%q is no file to attach, got %q", in, got)
		}
	}
}

func TestDroppingAFileAttachesAndSendsIt(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "general")
	f := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(f, pngBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	run(m, tea.PasteMsg{Content: strings.ReplaceAll(f, " ", `\ `)})
	if m.mode != insert || m.attach["C1"] == nil || m.chatIn.Value() != "" {
		t.Fatalf("drop: mode %v, attached %v, text %q", m.mode, m.attach["C1"] != nil, m.chatIn.Value())
	}
	if s := screen(m); !strings.Contains(s, "📎 shot.png") {
		t.Fatalf("the composer does not show the attachment:\n%s", s)
	}
	typeText(m, "the new dashboard")
	press(m, "enter")
	h := m.history["C1"]
	last := h[len(h)-1]
	if m.attach["C1"] != nil || last.Text != "the new dashboard" || len(last.Files) != 1 || last.Files[0].Name != "shot.png" || last.Files[0].W != 8 {
		t.Fatalf("sent: %+v (still attached %v)", last, m.attach["C1"] != nil)
	}
	// Text that is no file is pasted as text.
	run(m, tea.PasteMsg{Content: "just words"})
	if m.attach["C1"] != nil || m.chatIn.Value() != "just words" {
		t.Fatalf("a text paste: attached %v, text %q", m.attach["C1"] != nil, m.chatIn.Value())
	}
}

func TestPasteImageFromTheClipboard(t *testing.T) {
	m := newTest(t)
	was := clipboardFile
	t.Cleanup(func() { clipboardFile = was })
	pic := pngBytes(t)
	clipboardFile = func(context.Context) (string, []byte, error) { return "", pic, nil }
	m.goTo(t, "general")
	press(m, "i")
	run(m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if f := m.attach["C1"]; f == nil || !bytes.Equal(f.Data, pic) || !strings.HasSuffix(f.Name, ".png") {
		t.Fatalf("clipboard picture: %+v", f)
	}
	command_(m, "detach")
	if m.attach["C1"] != nil {
		t.Fatal(":detach kept it")
	}
	// Nothing to attach: ctrl+v is the usual paste of text.
	clipboardFile = func(context.Context) (string, []byte, error) { return "", nil, errNoClipboardImage }
	press(m, "i")
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if _, cmd2 := m.Update(cmd()); cmd2 == nil || m.attach["C1"] != nil || m.err != "" {
		t.Fatalf("no picture: should fall back to pasting text (err %q)", m.err)
	}
}

func TestAttachCommand(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "general")
	command_(m, "attach /no/such/file.png")
	if m.attach["C1"] != nil || !strings.Contains(m.err, "no such file") {
		t.Fatalf("missing file: %q", m.err)
	}
	f := filepath.Join(t.TempDir(), "notes.txt")
	_ = os.WriteFile(f, []byte("hello"), 0o600)
	command_(m, "attach "+f)
	if a := m.attach["C1"]; a == nil || a.Name != "notes.txt" {
		t.Fatalf(":attach: %+v %q", a, m.err)
	}
}

// slackUploads stands in for Slack: an upload's message is not told.
type slackUploads struct{ *chat.Fake }

func (s slackUploads) Upload(ctx context.Context, id, root, text string, f chat.Upload) (chat.Message, error) {
	_, err := s.Fake.Upload(ctx, id, root, text, f)
	return chat.Message{}, err
}

func TestUploadWithoutAMessageReloads(t *testing.T) {
	m := newModel(slackUploads{chat.NewFake()}, defaultKeymap(), Options{})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	m.goTo(t, "general")
	m.attach["C1"] = &chat.Upload{Name: "a.png", Data: pngBytes(t)}
	press(m, "i")
	press(m, "enter")
	h := m.history["C1"]
	if last := h[len(h)-1]; len(last.Files) != 1 || last.Files[0].Name != "a.png" {
		t.Fatalf("not reloaded after the upload: %+v", last)
	}
}

func TestParseAppleData(t *testing.T) {
	if b, err := parseAppleData("«data PNGf89504E47»\n"); err != nil || !bytes.Equal(b, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("%v %v", b, err)
	}
	if _, err := parseAppleData("execution error"); err == nil {
		t.Fatal("not a picture")
	}
}
