package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Every key of Config is documented in Reference, and nothing more.
func TestReferenceCoversConfig(t *testing.T) {
	doc := map[string]bool{}
	for _, f := range Reference {
		doc[f.Key] = true
	}
	typ := reflect.TypeFor[Config]()
	for i := range typ.NumField() {
		key, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if !doc[key] {
			t.Errorf("%s is not in Reference", key)
		}
		delete(doc, key)
	}
	for k := range doc {
		t.Errorf("Reference documents %s, which Config does not take", k)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	missing, found, err := Load(filepath.Join(dir, "none.json"))
	if err != nil || found || missing.Theme != "" {
		t.Fatalf("missing file: %+v %v %v", missing, found, err)
	}
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	c, found, err := Load(write("ok.json", `{"theme":"nord","sections":[{"name":"Team","channels":["a-*"],"collapsed":true}],"keys":{"react":["*"]}}`))
	if err != nil || !found || c.Theme != "nord" || len(c.Sections) != 1 || !c.Sections[0].Collapsed || c.Keys["react"][0] != "*" {
		t.Fatalf("ok: %+v %v %v", c, found, err)
	}
	if _, found, err := Load(write("typo.json", `{"thme":"nord"}`)); err == nil || !found {
		t.Fatalf("an unknown field must be an error, got %v %v", found, err)
	}
	if _, _, err := Load(write("bad.json", `{`)); err == nil {
		t.Fatal("bad JSON must be an error")
	}
	if _, _, err := Load(dir); err == nil {
		t.Fatal("a directory is not a config file")
	}
}

func TestPathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	if got := Path(); got != "/x/cfg/sltty/config.json" {
		t.Fatalf("Path = %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/h")
	if got := Path(); got != "/h/.config/sltty/config.json" {
		t.Fatalf("Path = %q", got)
	}
}
