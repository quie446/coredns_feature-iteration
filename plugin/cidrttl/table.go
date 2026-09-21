package cidrttl

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
)

// cidrEntry is a single validated row of the network-to-TTL mapping table.
type cidrEntry struct {
	network *net.IPNet
	ttl     uint32
	// rawText is the original CIDR notation as written in the table. It is used
	// in metrics and logs so observations stay stable across machines.
	rawText string
}

// table is an immutable, lookup-ready snapshot of the mapping table. Slices are
// kept sorted by prefix length (longest first) and then by network address, so
// overlapping entries resolve to the most specific match and lookups are
// deterministic on every machine.
type table struct {
	v4 []cidrEntry
	v6 []cidrEntry
}

// parseTable reads a mapping table. Every non-empty, non-comment line must have
// exactly two fields: a legal CIDR and a non-negative TTL. The first offending
// line aborts parsing and is reported with its 1-based line number, so callers
// can pinpoint the bad row. An empty table is rejected: silently accepting it
// would make a later "reload succeeded" report meaningless.
func parseTable(r io.Reader) (*table, error) {
	t := &table{}

	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("line %d: expected exactly 2 fields (CIDR and TTL), got %d: %q", lineNo, len(fields), line)
		}

		rawCIDR := fields[0]
		ip, network, err := net.ParseCIDR(rawCIDR)
		if err != nil {
			return nil, fmt.Errorf("line %d: illegal CIDR notation %q: %v", lineNo, rawCIDR, err)
		}
		// Reject host bits set in the network field, e.g. 10.0.0.1/24. Such rows
		// are almost always typos and would silently match a different network.
		if !network.IP.Equal(ip) {
			return nil, fmt.Errorf("line %d: CIDR %q has non-zero host bits; expected %s", lineNo, rawCIDR, network.String())
		}

		ttl, err := parseTTL(fields[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", lineNo, err)
		}

		entry := cidrEntry{network: network, ttl: ttl, rawText: network.String()}
		if ip.To4() != nil {
			t.v4 = append(t.v4, entry)
		} else {
			t.v6 = append(t.v6, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed reading mapping table: %v", err)
	}

	if len(t.v4)+len(t.v6) == 0 {
		return nil, fmt.Errorf("mapping table is empty: at least one CIDR/TTL entry is required")
	}

	sortEntries(t.v4)
	sortEntries(t.v6)
	return t, nil
}

// parseTTL accepts only an explicit non-negative 32-bit integer. A blank,
// negative, fractional or otherwise non-numeric value is an error rather than a
// default.
func parseTTL(s string) (uint32, error) {
	if strings.TrimSpace(s) == "" {
		return 0, fmt.Errorf("TTL is blank")
	}
	if strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("TTL %q must not be negative", s)
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("illegal TTL %q: %v", s, err)
	}
	return uint32(v), nil
}

func sortEntries(entries []cidrEntry) {
	sort.Slice(entries, func(i, j int) bool {
		onesI, _ := entries[i].network.Mask.Size()
		onesJ, _ := entries[j].network.Mask.Size()
		if onesI != onesJ {
			return onesI > onesJ
		}
		return entries[i].network.IP.String() < entries[j].network.IP.String()
	})
}

// lookup returns the most specific entry containing ip. Overlapping CIDRs
// therefore always resolve to the longest-prefix match instead of the first row
// encountered in the file.
func (t *table) lookup(ip net.IP) (cidrEntry, bool) {
	entries := t.v6
	if ip.To4() != nil {
		entries = t.v4
	}
	for _, entry := range entries {
		if entry.network.Contains(ip) {
			return entry, true
		}
	}
	return cidrEntry{}, false
}
