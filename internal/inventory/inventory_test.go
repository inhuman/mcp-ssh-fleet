package inventory

import "testing"

const sample = `
hosts:
  - name: vm-a1
    addr: 10.0.0.11
    user: root
    tags: [Stand-A, Test]
  - name: vm-a2
    addr: 10.0.0.12
    user: ubuntu
    port: 2222
    tags: [stand-a]
  - name: vm-b1
    addr: 10.0.0.21
    user: admin
    tags: [stand-b, test]
`

func mustParse(t *testing.T, s string) *Inventory {
	t.Helper()
	inv, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return inv
}

func TestParse_DefaultsAndNormalize(t *testing.T) {
	inv := mustParse(t, sample)
	if inv.Len() != 3 {
		t.Fatalf("want 3 hosts, got %d", inv.Len())
	}
	a1, _ := inv.Resolve("vm-a1")
	if a1.Port != 22 {
		t.Fatalf("default port want 22, got %d", a1.Port)
	}
	// tags are normalized to lower-case and sorted
	if got := a1.Tags; len(got) != 2 || got[0] != "stand-a" || got[1] != "test" {
		t.Fatalf("tags normalize failed: %v", got)
	}
	a2, _ := inv.Resolve("vm-a2")
	if a2.Port != 2222 {
		t.Fatalf("explicit port want 2222, got %d", a2.Port)
	}
}

func TestParse_Rejects(t *testing.T) {
	cases := map[string]string{
		"dup name":   "hosts:\n  - {name: x, addr: 10.0.0.1, user: root}\n  - {name: x, addr: 10.0.0.2, user: root}\n",
		"empty addr": "hosts:\n  - {name: x, addr: '', user: root}\n",
		"empty user": "hosts:\n  - {name: x, addr: 10.0.0.1, user: ''}\n",
		"bad port":   "hosts:\n  - {name: x, addr: 10.0.0.1, user: root, port: 70000}\n",
		"empty name": "hosts:\n  - {name: '', addr: 10.0.0.1, user: root}\n",
	}
	for name, y := range cases {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestParse_EmptyInventoryOK(t *testing.T) {
	inv := mustParse(t, "hosts: []\n")
	if inv.Len() != 0 {
		t.Fatalf("empty inventory should parse, got %d hosts", inv.Len())
	}
	if got := inv.Match([]string{"any"}); len(got) != 0 {
		t.Fatalf("empty inventory match should be empty, got %d", len(got))
	}
}

func TestMatch_AND(t *testing.T) {
	inv := mustParse(t, sample)
	// a single tag 'test' matches vm-a1 and vm-b1
	if got := inv.Match([]string{"test"}); len(got) != 2 {
		t.Fatalf("tag test: want 2, got %d", len(got))
	}
	// AND: stand-a AND test matches vm-a1 only
	got := inv.Match([]string{"stand-a", "test"})
	if len(got) != 1 || got[0].Name != "vm-a1" {
		t.Fatalf("AND stand-a+test: want [vm-a1], got %+v", got)
	}
	// the query is case-insensitive
	if got := inv.Match([]string{"STAND-A"}); len(got) != 2 {
		t.Fatalf("case-insensitive stand-a: want 2, got %d", len(got))
	}
	// an unknown tag matches nothing
	if got := inv.Match([]string{"nope"}); len(got) != 0 {
		t.Fatalf("unknown tag: want 0, got %d", len(got))
	}
}

func TestResolve_NameAddrFailClosed(t *testing.T) {
	inv := mustParse(t, sample)
	if _, ok := inv.Resolve("vm-a2"); !ok {
		t.Fatal("resolve by name failed")
	}
	if _, ok := inv.Resolve("10.0.0.21"); !ok {
		t.Fatal("resolve by addr failed")
	}
	if _, ok := inv.Resolve("10.9.9.9"); ok {
		t.Fatal("host outside inventory must NOT resolve (fail-closed)")
	}
}
