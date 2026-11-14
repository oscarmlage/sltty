package chat

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStripControlNeutralisesEscapes(t *testing.T) {
	in := []byte(`{"text":"a\u001b]52;c;eHl6\u0007b\u009bc\nd\te\u001B"}`)
	var out struct{ Text string }
	if err := json.Unmarshal(StripControl(in), &out); err != nil {
		t.Fatal(err)
	}
	for _, r := range out.Text {
		if r != '\n' && r != '\t' && (r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)) {
			t.Fatalf("control character %q survived in %q", r, out.Text)
		}
	}
	if !strings.Contains(out.Text, "\n") || !strings.Contains(out.Text, "\t") {
		t.Fatalf("newline and tab must stay: %q", out.Text)
	}
}

func TestUnescapeDoesNotSmuggleControls(t *testing.T) {
	if got := unescape("x&#27;[31m&#0;y &lt;ok&gt;"); strings.ContainsAny(got, "\x1b\x00") || !strings.Contains(got, "<ok>") {
		t.Fatalf("unescape = %q", got)
	}
}

func TestSafeLink(t *testing.T) {
	for in, want := range map[string]string{"https://a.b": "https://a.b", "http://a.b": "http://a.b", "javascript:x": "", "file:///etc/passwd": "", "": ""} {
		if got := safeLink(in); got != want {
			t.Errorf("safeLink(%q) = %q, want %q", in, got, want)
		}
	}
}
