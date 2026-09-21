package cidrttl

import (
	"net"
	"strings"
	"testing"
)

func mustTable(t *testing.T, content string) *table {
	t.Helper()
	tab, err := parseTable(strings.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	return tab
}

func TestParseTableValid(t *testing.T) {
	// A previously accepted table must keep loading after validation tightened.
	content := strings.Join([]string{
		"# comment line",
		"",
		"10.0.0.0/8 60",
		"10.1.0.0/16 30",
		"2001:db8::/32 120",
		"192.168.1.1/32 0",
	}, "\n")
	tab := mustTable(t, content)
	if len(tab.v4) != 3 || len(tab.v6) != 1 {
		t.Fatalf("unexpected entry counts: v4=%d v6=%d", len(tab.v4), len(tab.v6))
	}

	ones, _ := tab.v4[0].network.Mask.Size()
	if ones != 32 {
		t.Fatalf("expected most specific /32 first, got /%d", ones)
	}
}

func TestParseTableErrors(t *testing.T) {
	cases := map[string]string{
		"missing field":      "10.0.0.0/8\n",
		"illegal cidr":       "10.0.0.0/99 60\n",
		"host bits set":      "10.0.0.1/24 60\n",
		"negative ttl":       "10.0.0.0/8 -5\n",
		"blank ttl":          "10.0.0.0/8   \n",
		"non numeric ttl":    "10.0.0.0/8 abc\n",
		"extra field":        "10.0.0.0/8 60 extra\n",
		"empty table":        "# only a comment\n",
		"error reports line": "10.0.0.0/8 60\nnot-a-cidr 30\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseTable(strings.NewReader(content))
			if err == nil {
				t.Fatalf("expected error for %q", name)
			}
			if name == "error reports line" && !strings.Contains(err.Error(), "line 2") {
				t.Fatalf("expected error to identify line 2, got: %v", err)
			}
		})
	}
}

func TestLookupLongestPrefix(t *testing.T) {
	content := "10.0.0.0/8 60\n10.1.0.0/16 30\n10.1.2.0/24 10\n"
	tab := mustTable(t, content)

	cases := []struct {
		ip   string
		ttl  uint32
		cidr string
		hit  bool
	}{
		{"10.1.2.3", 10, "10.1.2.0/24", true},
		{"10.1.9.9", 30, "10.1.0.0/16", true},
		{"10.9.9.9", 60, "10.0.0.0/8", true},
		{"11.0.0.1", 0, "", false},
	}
	for _, tc := range cases {
		entry, ok := tab.lookup(net.ParseIP(tc.ip))
		if ok != tc.hit {
			t.Fatalf("ip %s: hit=%v want %v", tc.ip, ok, tc.hit)
		}
		if ok && (entry.ttl != tc.ttl || entry.rawText != tc.cidr) {
			t.Fatalf("ip %s: got ttl=%d cidr=%s want ttl=%d cidr=%s", tc.ip, entry.ttl, entry.rawText, tc.ttl, tc.cidr)
		}
	}
}

func TestLookupOrderIndependent(t *testing.T) {
	// Same entries in reverse file order must produce the same sorted table.
	a := mustTable(t, "10.0.0.0/8 60\n10.1.0.0/16 30\n")
	b := mustTable(t, "10.1.0.0/16 30\n10.0.0.0/8 60\n")
	ip := net.ParseIP("10.1.1.1")
	ea, _ := a.lookup(ip)
	eb, _ := b.lookup(ip)
	if ea.ttl != eb.ttl || ea.rawText != eb.rawText {
		t.Fatalf("lookup depends on file order: %d/%s vs %d/%s", ea.ttl, ea.rawText, eb.ttl, eb.rawText)
	}
}
