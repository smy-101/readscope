package browser

import (
	"runtime"
	"testing"
)

func TestEnvOverrideWins(t *testing.T) {
	t.Setenv("READSCOPE_BROWSER", "firejure --new-window")
	cmd := Command("https://example.com")
	want := []string{"firejure", "--new-window", "https://example.com"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("args = %v, want %v", cmd.Args, want)
		}
	}
}

func TestDefaultCommandResolves(t *testing.T) {
	cmd := Command("https://example.com")
	if cmd == nil {
		t.Fatal("a default command must exist")
	}
	if len(cmd.Args) < 2 || cmd.Args[len(cmd.Args)-1] != "https://example.com" {
		t.Fatalf("url must be the final argument: %v", cmd.Args)
	}
	// On Linux the opener is xdg-open or wslview (WSL with wslu).
	if runtime.GOOS == "linux" {
		opener := cmd.Args[0]
		if opener != "xdg-open" && opener != "wslview" {
			t.Fatalf("linux opener = %q, want xdg-open or wslview", opener)
		}
	}
}
