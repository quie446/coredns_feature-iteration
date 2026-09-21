package ttlrewrite

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coredns/caddy"
)

func writeTable(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSetupOK(t *testing.T) {
	path := writeTable(t, "10.0.0.0/8 30\n")
	c := caddy.NewTestController("dns", "ttlrewrite "+path)
	if err := setup(c); err != nil {
		t.Fatalf("expected setup to succeed, got: %v", err)
	}
}

func TestSetupFailsOnInvalidTable(t *testing.T) {
	tables := []string{
		"10.0.0.0/8 -1\n",  // negative ttl
		"10.0.0.0/8\n",     // missing ttl
		"bogus/8 30\n",     // invalid network
		"# nothing here\n", // empty table must not count as active
	}
	for _, table := range tables {
		path := writeTable(t, table)
		c := caddy.NewTestController("dns", "ttlrewrite "+path)
		if err := setup(c); err == nil {
			t.Fatalf("expected setup to fail for table %q", table)
		}
	}
}

func TestSetupFailsOnMissingFile(t *testing.T) {
	c := caddy.NewTestController("dns", "ttlrewrite /nonexistent/rules.txt")
	if err := setup(c); err == nil {
		t.Fatal("expected setup to fail for missing file")
	}
}

func TestSetupFailsOnMissingArgument(t *testing.T) {
	c := caddy.NewTestController("dns", "ttlrewrite")
	if err := setup(c); err == nil {
		t.Fatal("expected setup to fail without a rule table file")
	}
}

func TestParseReloadOption(t *testing.T) {
	path := writeTable(t, "10.0.0.0/8 30\n")
	c := caddy.NewTestController("dns", "ttlrewrite "+path+" {\nreload 5s\n}\n")
	gotPath, interval, err := parse(c)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path {
		t.Fatalf("expected path %s, got %s", path, gotPath)
	}
	if interval != 5*time.Second {
		t.Fatalf("expected interval 5s, got %s", interval)
	}
}

func TestParseRejectsBadReload(t *testing.T) {
	path := writeTable(t, "10.0.0.0/8 30\n")
	for _, block := range []string{"reload -1s", "reload 0s", "reload bogus", "reload"} {
		c := caddy.NewTestController("dns", "ttlrewrite "+path+" {\n"+block+"\n}\n")
		if _, _, err := parse(c); err == nil {
			t.Fatalf("expected parse to fail for %q", block)
		}
	}
}
