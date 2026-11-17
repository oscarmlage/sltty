package main

import (
	"runtime/debug"
	"strings"
)

// buildVersion is the version and the commit the binary was built from.
// The Makefile sets version (git describe); a plain go build leaves "dev",
// and the commit comes from what Go records of the repository (vcs.*),
// "-dirty" when it had uncommitted changes.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	return describe(version, info.Settings)
}

func describe(v string, settings []debug.BuildSetting) string {
	var rev, at string
	dirty := false
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			at = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return v
	}
	short := rev[:min(len(rev), 7)]
	if dirty {
		short += "-dirty"
	}
	if strings.Contains(v, rev[:min(len(rev), 7)]) {
		return v // git describe already names it
	}
	if at != "" {
		short += ", " + strings.Replace(strings.TrimSuffix(at, "Z"), "T", " ", 1) + " UTC"
	}
	return v + " (" + short + ")"
}
