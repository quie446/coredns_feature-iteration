package ttlrewrite

import (
	"net"
	"strings"
	"testing"
)

func mustParse(t *testing.T, table string) *ruleSet {
	t.Helper()
	rs, err := parseRules(strings.NewReader(table))
	if err != nil {
		t.Fatalf("expected table to load, got error: %v", err)
	}
	return rs
}

func TestParseValidTable(t *testing.T) {
	rs := mustParse(t, "# comment\n\n10.0.0.0/8 30\n192.168.0.0/16 0\n2001:db8::/32 3600\n")
	if rs.len() != 3 {
		t.Fatalf("expected 3 rules, got %d", rs.len())
	}
}

func TestParseFailures(t *testing.T) {
	tables := map[string]string{
		"missing ttl":     "10.0.0.0/8\n",
		"blank ttl":       "10.0.0.0/8 \n",
		"negative ttl":    "10.0.0.0/8 -1\n",
		"non-numeric ttl": "10.0.0.0/8 abc\n",
		"overflow ttl":    "10.0.0.0/8 4294967296\n",
		"invalid network": "10.0.0.0/33 30\n",
		"garbage network": "not-a-network 30\n",
		"extra field":     "10.0.0.0/8 30 extra\n",
		"empty table":     "# only comments\n\n",
		"error on line 3": "10.0.0.0/8 30\n192.168.0.0/16 60\n172.16.0.0/12 -5\n",
	}
	for name, table := range tables {
		t.Run(name, func(t *testing.T) {
			_, err := parseRules(strings.NewReader(table))
			if err == nil {
				t.Fatalf("expected load to fail for %q", table)
			}
		})
	}
}

func TestParseFailureReportsLine(t *testing.T) {
	_, err := parseRules(strings.NewReader("10.0.0.0/8 30\n# ok\n\n172.16.0.0/12 -5\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("error should report the failing line, got: %v", err)
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Fatalf("error should report the reason, got: %v", err)
	}
}

func TestLongestPrefixWins(t *testing.T) {
	// Broader network listed first: match must still prefer the more
	// specific overlapping rule.
	rs := mustParse(t, "10.0.0.0/8 100\n10.1.0.0/16 200\n10.1.2.0/24 300\n")
	tests := []struct {
		ip  string
		ttl uint32
	}{
		{"10.1.2.3", 300},
		{"10.1.3.3", 200},
		{"10.2.0.1", 100},
	}
	for _, tc := range tests {
		rule := rs.match(net.ParseIP(tc.ip))
		if rule == nil {
			t.Fatalf("expected match for %s", tc.ip)
		}
		if rule.TTL != tc.ttl {
			t.Fatalf("ip %s: expected ttl %d, got %d", tc.ip, tc.ttl, rule.TTL)
		}
	}
}

func TestNoMatch(t *testing.T) {
	rs := mustParse(t, "10.0.0.0/8 100\n")
	if rule := rs.match(net.ParseIP("192.0.2.1")); rule != nil {
		t.Fatalf("expected no match, got %v", rule)
	}
}

func TestMatchDeterministicAcrossRuns(t *testing.T) {
	table := "10.0.0.0/8 100\n10.1.0.0/16 200\n10.1.2.0/24 300\n192.168.0.0/16 50\n"
	want := mustParse(t, table).match(net.ParseIP("10.1.2.3")).TTL
	for i := 0; i < 50; i++ {
		got := mustParse(t, table).match(net.ParseIP("10.1.2.3")).TTL
		if got != want {
			t.Fatalf("run %d: match flipped from %d to %d", i, want, got)
		}
	}
}
