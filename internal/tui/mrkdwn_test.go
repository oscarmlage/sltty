package tui

import "testing"

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
