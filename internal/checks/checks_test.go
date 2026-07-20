package checks

import (
	"slices"
	"strings"
	"testing"
)

func TestResolve_Known(t *testing.T) {
	for _, name := range Names() {
		c, err := Resolve(name)
		if err != nil {
			t.Fatalf("resolve %q: %v", name, err)
		}
		if c.Script == "" {
			t.Fatalf("check %q has empty script", name)
		}
	}
	// case-insensitive
	if _, err := Resolve("DISK"); err != nil {
		t.Fatalf("case-insensitive resolve failed: %v", err)
	}
}

func TestResolve_UnknownListsAvailable(t *testing.T) {
	_, err := Resolve("rm-rf")
	if err == nil {
		t.Fatal("unknown check must error")
	}
	for _, name := range Names() {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error should list available check %q: %v", name, err)
		}
	}
}

func TestNames_Sorted(t *testing.T) {
	got := Names()
	if !slices.IsSorted(got) {
		t.Fatalf("Names not sorted: %v", got)
	}
	if len(got) < 5 {
		t.Fatalf("expected MVP set of >=5 checks, got %d", len(got))
	}
}

func TestScripts_NonInteractive(t *testing.T) {
	for _, name := range Names() {
		c, _ := Resolve(name)
		// curated checks must never need interactive input or privilege escalation
		for _, bad := range []string{"sudo -S", "read ", "vi ", "vim ", "less ", "top\n"} {
			if strings.Contains(c.Script, bad) {
				t.Errorf("check %q script looks interactive/escalating: %q", name, c.Script)
			}
		}
	}
}
