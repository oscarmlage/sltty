package config

import (
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
