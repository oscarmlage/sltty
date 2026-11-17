package main

import (
	"runtime/debug"
	"testing"
)

func TestDescribe(t *testing.T) {
	rev := "e699077abcdef0123456789"
	set := func(modified string) []debug.BuildSetting {
		return []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.time", Value: "2026-05-04T10:20:30Z"}, {Key: "vcs.modified", Value: modified}}
	}
	for _, tc := range []struct {
		v        string
		settings []debug.BuildSetting
		want     string
	}{
		{"dev", set("false"), "dev (e699077, 2026-05-04 10:20:30 UTC)"},
		{"dev", set("true"), "dev (e699077-dirty, 2026-05-04 10:20:30 UTC)"},
		{"v0.3.0-4-ge699077", set("false"), "v0.3.0-4-ge699077"},
		{"dev", nil, "dev"},
	} {
		if got := describe(tc.v, tc.settings); got != tc.want {
			t.Errorf("describe(%q) = %q, want %q", tc.v, got, tc.want)
		}
	}
}
