package ttlrewrite

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
)

// Rule maps a client source network to the TTL that should be stamped on
// positive answers returned to clients in that network.
type Rule struct {
	Network *net.IPNet
	TTL     uint32
	// order is the position of the rule in the source table. It makes the
	// longest-prefix match deterministic when two rules are equally specific.
	order int
	// raw is the original "cidr ttl" text, used in logs and error messages.
	raw string
}

// ruleSet is an immutable, sorted view of the loaded rules. The handler swaps
// ruleSets atomically on reload, so lookups never see a half-loaded table.
type ruleSet struct {
	rules []Rule
}

// parseRules reads a subnet table of the form "<cidr> <ttl-seconds>" per line.
// Empty lines and lines starting with '#' are ignored. Every malformed entry
// aborts the load and reports the exact line that failed, so a broken table
// can never be activated silently.
func parseRules(r io.Reader) (*ruleSet, error) {
	var rules []Rule
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule, err := parseRule(line, len(rules))
		if err != nil {
			return nil, fmt.Errorf("line %d (%q): %w", lineNo, line, err)
		}
		rules = append(rules, rule)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading rule table: %w", err)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("rule table is empty: no usable subnet/TTL entries found")
	}
	return newRuleSet(rules), nil
}

func parseRule(line string, order int) (Rule, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return Rule{}, fmt.Errorf("missing required field: expected \"<cidr> <ttl>\", got %d field(s)", len(fields))
	}
	if len(fields) > 2 {
		return Rule{}, fmt.Errorf("unexpected extra field(s): expected exactly \"<cidr> <ttl>\"")
	}
	_, network, err := net.ParseCIDR(fields[0])
	if err != nil {
		return Rule{}, fmt.Errorf("invalid network %q: %w", fields[0], err)
	}
	ttl, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
	if err != nil {
		return Rule{}, fmt.Errorf("invalid ttl %q: %w", fields[1], err)
	}
	if ttl < 0 {
		return Rule{}, fmt.Errorf("ttl must not be negative: %d", ttl)
	}
	if ttl > 0xFFFFFFFF {
		return Rule{}, fmt.Errorf("ttl %d exceeds maximum 4294967295", ttl)
	}
	return Rule{Network: network, TTL: uint32(ttl), order: order, raw: fields[0] + " " + fields[1]}, nil
}

// newRuleSet sorts rules so that more specific networks are matched first.
// The sort is stable on the original file order, making lookups deterministic
// across runs and machines.
func newRuleSet(rules []Rule) *ruleSet {
	sorted := make([]Rule, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		si, _ := sorted[i].Network.Mask.Size()
		sj, _ := sorted[j].Network.Mask.Size()
		return si > sj
	})
	return &ruleSet{rules: sorted}
}

// match returns the most specific rule containing ip, or nil when no rule
// applies. Overlapping networks resolve by longest prefix, never by whichever
// rule happened to be listed first.
func (rs *ruleSet) match(ip net.IP) *Rule {
	if rs == nil {
		return nil
	}
	for i := range rs.rules {
		if rs.rules[i].Network.Contains(ip) {
			return &rs.rules[i]
		}
	}
	return nil
}

func (rs *ruleSet) len() int {
	if rs == nil {
		return 0
	}
	return len(rs.rules)
}
