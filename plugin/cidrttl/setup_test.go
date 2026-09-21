package cidrttl

import (
	"testing"

	"github.com/coredns/caddy"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"minimal", `cidrttl /tmp/cidr.ttl`, false},
		{"reload disabled", "cidrttl /tmp/cidr.ttl {\nreload 0\n}", false},
		{"reload duration", "cidrttl /tmp/cidr.ttl {\nreload 5s\n}", false},
		{"no file", `cidrttl`, true},
		{"unknown property", "cidrttl /tmp/cidr.ttl {\nfoo bar\n}", true},
		{"negative reload", "cidrttl /tmp/cidr.ttl {\nreload -1s\n}", true},
		{"bad duration", "cidrttl /tmp/cidr.ttl {\nreload xyz\n}", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := caddy.NewTestController("dns", tc.input)
			ct, _, err := parseConfig(c)
			if tc.wantErr && err == nil {
				t.Fatal("expected setup error")
			}
			if !tc.wantErr && (err != nil || ct == nil) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
