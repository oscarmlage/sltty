package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSlackMrkdwnHostileInput(t *testing.T) {
	for _, in := range []string{"hi \x0099\x00", "\x000\x00", "<https://x|a\x00b>", "*\x00*", "```\x00```"} {
		slackMrkdwn(in) // must not panic
	}
}

func FuzzSlackMrkdwn(f *testing.F) {
	for _, s := range []string{"*b* _i_ ~s~ `c`", "<https://a|b> <@U1> <!here>", "[a](https://b)", "```x```", "\x000\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { slackMrkdwn(s) })
}

func TestBareURLsAreLinks(t *testing.T) {
	newTest(t) // the styles
	long := "https://gitlab.flywire.tech/flywire/sre/terraform_module_bedrock_inference_profile/-/merge_requests/23"
	for _, tc := range []struct{ in, url, shown, after string }{
		{long, long, "gitlab.flywire.tech/flywire/…/23", ""},
		{"mira https://x.io/a_b_c.", "https://x.io/a_b_c", "https://x.io/a_b_c", "."},
		{"(https://x.io/y)", "https://x.io/y", "https://x.io/y", ")"},
	} {
		out := slackMrkdwn(tc.in)
		if !strings.Contains(out, "\x1b]8;;"+tc.url+"\x1b\\") {
			t.Errorf("%q: no hyperlink to %q in %q", tc.in, tc.url, out)
		}
		if got := ansi.Strip(out); !strings.Contains(got, tc.shown+tc.after) {
			t.Errorf("%q shows %q, want %q", tc.in, got, tc.shown+tc.after)
		}
	}
	// Inside code it stays code; a labelled link is not linked twice.
	if out := slackMrkdwn("`https://x.io/y`"); strings.Contains(out, "\x1b]8;;") {
		t.Errorf("code became a link: %q", out)
	}
	if out := slackMrkdwn("[l](https://x.io/y)"); strings.Count(out, "\x1b]8;;https") != 1 {
		t.Errorf("labelled link: %q", out)
	}
}
