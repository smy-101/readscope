package i18n

import (
	"reflect"
	"regexp"
	"strings"
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

// verbRe matches one fmt verb, indexed or not, with any flag,
// width, or precision ("%d", "%[2]s", "%-3.2f", "% d").
var verbRe = regexp.MustCompile(`%(\[[0-9]+\])?[-+#0-9. ]*[a-zA-Z]`)

// verbs is a message's verb sequence in order: with unindexed verbs
// Sprintf fills arguments in that order, so a translation whose
// sequence differs from the default pack's is a runtime "%!d/string"
// blow-up waiting for its first caller. "%%" (literal percent) is
// stripped first so "%%d"-shaped text can't mint a phantom verb.
func verbs(msg string) []string {
	return verbRe.FindAllString(strings.ReplaceAll(msg, "%%", ""), -1)
}

// TestParityFormatVerbsMatch pins the other half of translation
// parity: the key-tree test above proves every key exists in every
// pack, this one proves every key formats the same arguments. A zh
// "%d 条" paired with an en "%s items" would otherwise pass the
// key tree and only explode the first time the string is fetched.
func TestParityFormatVerbsMatch(t *testing.T) {
	base, err := Load(Default)
	if err != nil {
		t.Fatalf("Load(%q): %v", Default, err)
	}
	infos, err := Available()
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	for _, info := range infos {
		c, err := Load(info.Code)
		if err != nil {
			t.Fatalf("Load(%q): %v", info.Code, err)
		}
		for _, key := range c.Keys() {
			want := verbs(base.msgs[key])
			got := verbs(c.msgs[key])
			if !reflect.DeepEqual(want, got) {
				t.Errorf("%s: %s verb sequence %v, %s has %v\n  default: %q\n  %s: %q",
					Default, key, want, info.Code, got, base.msgs[key], info.Code, c.msgs[key])
			}
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
