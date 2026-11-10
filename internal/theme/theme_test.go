package theme

import "testing"

func TestLoadBuiltins(t *testing.T) {
	for _, name := range Available() {
		th, err := Load(name)
		if err != nil {
			t.Fatalf("Load(%q): %v", name, err)
		}
		if th.Name != name {
			t.Errorf("Load(%q).Name = %q", name, th.Name)
		}
	}
}

func TestAvailableHasDarkAndLight(t *testing.T) {
	names := Available()
	want := []string{"dark", "light"}
	if len(names) != len(want) {
		t.Fatalf("Available() = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("Available() = %v, want %v", names, want)
		}
	}
}

func TestLoadUnknownFallsBackToDefault(t *testing.T) {
	th, err := Load("solarized")
	if err != nil {
		t.Fatalf("Load(unknown): %v", err)
	}
	if th.Name != Default {
		t.Fatalf("expected fallback to %q, got %q", Default, th.Name)
	}
}
