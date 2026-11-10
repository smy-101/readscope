package i18n

import (
	"reflect"
	"testing"
)

func TestParityAllCatalogsShareKeyTree(t *testing.T) {
	infos, err := Available()
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if len(infos) < 2 {
		t.Fatalf("expected at least 2 language packs, got %d", len(infos))
	}
	base, err := Load(Default)
	if err != nil {
		t.Fatalf("Load(%q): %v", Default, err)
	}
	baseKeys := base.Keys()
	for _, info := range infos {
		c, err := Load(info.Code)
		if err != nil {
			t.Fatalf("Load(%q): %v", info.Code, err)
		}
		if c.Name == "" {
			t.Errorf("%s: [meta] name empty", info.Code)
		}
		keys := c.Keys()
		if !reflect.DeepEqual(baseKeys, keys) {
			t.Errorf("%s: key tree differs from %s\n  missing: %v\n  extra:   %v",
				info.Code, Default, diff(baseKeys, keys), diff(keys, baseKeys))
		}
	}
}

func TestLoadUnknownCodeFallsBackToDefault(t *testing.T) {
	c, err := Load("xx-YY")
	if err != nil {
		t.Fatalf("Load(unknown): %v", err)
	}
	if c.Code != Default {
		t.Fatalf("expected fallback to %q, got %q", Default, c.Code)
	}
}

func TestGetFormatsArgs(t *testing.T) {
	c, err := Load("en")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := c.Get("doctor.summary", 3, 2, 1); got != "3 checks: 2 ok, 1 failed" {
		t.Fatalf("Get with args: %q", got)
	}
	if got := c.Get("app.title"); got != "readscope" {
		t.Fatalf("Get plain: %q", got)
	}
}

func TestGetMissingKeyReturnsKey(t *testing.T) {
	c, err := Load("en")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := c.Get("no.such.key"); got != "no.such.key" {
		t.Fatalf("missing key should return itself, got %q", got)
	}
}

func diff(want, got []string) []string {
	set := make(map[string]struct{}, len(got))
	for _, k := range got {
		set[k] = struct{}{}
	}
	var out []string
	for _, k := range want {
		if _, ok := set[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}
