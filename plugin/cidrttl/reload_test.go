package cidrttl

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write table: %v", err)
	}
}

func TestLoadSuccessAndFailureKeepsOldTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cidr.ttl")
	writeTable(t, path, "10.0.0.0/8 60\n")

	c := &CidrTTL{path: path}
	if err := c.load("initial load"); err != nil {
		t.Fatalf("initial load failed: %v", err)
	}

	// Break the table with a negative TTL. Reload must fail, report the reason,
	// and leave the previous table serving.
	writeTable(t, path, "10.0.0.0/8 -1\n")
	if err := c.load("reload"); err == nil {
		t.Fatal("expected reload to fail on negative TTL")
	}

	tab := c.current.Load()
	if tab == nil {
		t.Fatal("previous table disappeared after failed reload")
	}
	if got := tab.v4[0].ttl; got != 60 {
		t.Fatalf("old table mutated by failed reload, ttl=%d", got)
	}

	// Empty table must never be accepted.
	writeTable(t, path, "# nothing here\n")
	if err := c.load("reload"); err == nil {
		t.Fatal("expected empty table reload to fail")
	}

	// Valid new table loads and takes effect.
	writeTable(t, path, "10.0.0.0/8 120\n10.1.0.0/16 30\n")
	if err := c.load("reload"); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	tab = c.current.Load()
	if len(tab.v4) != 2 || tab.v4[0].ttl != 30 {
		t.Fatalf("new table did not take effect: %+v", tab.v4)
	}
}

func TestInitialLoadMissingFileFails(t *testing.T) {
	c := &CidrTTL{path: filepath.Join(t.TempDir(), "missing")}
	if err := c.load("initial load"); err == nil {
		t.Fatal("expected missing file to fail initial load")
	}
}

func TestHotReloadPicksUpChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cidr.ttl")
	writeTable(t, path, "10.0.0.0/8 60\n")

	c := &CidrTTL{path: path, stop: make(chan struct{})}
	if err := c.load("initial load"); err != nil {
		t.Fatal(err)
	}

	c.startReloader(20 * time.Millisecond)
	defer c.stopReloader()

	writeTable(t, path, "10.0.0.0/8 5\n")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.current.Load().v4[0].ttl == 5 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("hot-reloaded table did not take effect")
}
