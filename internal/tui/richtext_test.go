package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
)

func TestMarksStayOutOfLinksAndCode(t *testing.T) {
	const url = "https://git.test/orbit/-/merge_requests/1379"
	s := slackMrkdwn("on [!1379](" + url + ") `eval_blobs.delete` for run_if roots, _now_")
	if !strings.Contains(s, osc8(url, "!1379")) {
		t.Fatalf("the link's URL was marked into: %q", s)
	}
	if strings.Count(s, "\x1b[3m") != 1 || !strings.Contains(s, "\x1b[3mnow\x1b[23m") {
		t.Fatalf("italics only for _now_, not inside a word or code: %q", s)
	}
	if got := ansi.Strip(s); got != "on !1379 eval_blobs.delete for run_if roots, now" {
		t.Fatalf("reads %q", got)
	}
}

func TestBroadcastThreadReplyHasContext(t *testing.T) {
	at := time.Date(2025, 1, 2, 9, 0, 0, 0, time.UTC)
	msgs := []chat.Message{
		{TS: "1", User: "ana", At: at, Text: "Review the deployment plan before lunch"},
		{TS: "2", User: "bob", At: at.Add(time.Minute), Text: "Looks good", Broadcast: true, ThreadRoot: "1"},
	}
	lines, _ := renderMsgs(msgs, 60, false, "you", at, "", nil, stubArt{})
	got := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(got, "↳ replied to a thread: Review the deployment plan") {
		t.Fatalf("broadcast context missing:\n%s", got)
	}
}

func TestSentryReadsAsInSlack(t *testing.T) {
	const url = "https://sentry.test/issues/1/?a=1&b=2"
	s := slackMrkdwn(":red_circle: [*ActivityError*](" + url + ")\n```GitLab rejected 'x' -> 'y'\nsee https://git.test/a_b_c```\nenvironment: `production`")
	if !strings.Contains(s, osc8(url, "ActivityError")) {
		t.Fatalf("a bold link reads as a link: %q", s)
	}
	want := ":red_circle: ActivityError\nGitLab rejected 'x' -> 'y'\nsee https://git.test/a_b_c\nenvironment: production"
	if got := ansi.Strip(s); got != want {
		t.Fatalf("reads\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(s, "\x1b]8;;https://git.test") {
		t.Fatal("a URL inside a code block is code, not a link")
	}
}

func TestBotMessageReadsAsInSlack(t *testing.T) {
	at := time.Date(2026, 11, 13, 9, 3, 0, 0, time.Local)
	msgs := []chat.Message{
		{TS: "1", User: "claude", UserID: "U1", At: at, Text: "starting"},
		{TS: "2", User: "Claude [daily run]", UserID: "B1", At: at.Add(10 * time.Minute),
			Text: "_Daily_\n• one\n• two", Context: []string{"sonnet · Configure"}},
	}
	lines, _ := renderMsgs(msgs, 60, false, "you", at, "", nil, stubArt{})
	got := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{
		" [B1] Claude [daily run]  09:13",
		" [B1] _Daily_",
		"      • one",
		"      • two",
		"      sonnet · Configure",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("no %q in\n%s", want, got)
		}
	}
}
