package ttlrewrite

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"

	"github.com/miekg/dns"
)

func waitForTTL(t *testing.T, p *TTLRewrite, remote string, want uint32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		rec := dnstest.NewRecorder(&test.ResponseWriter{RemoteIP: remote})
		req := new(dns.Msg)
		req.SetQuestion("example.org.", dns.TypeA)
		if _, err := p.ServeDNS(context.TODO(), rec, req); err != nil {
			t.Fatal(err)
		}
		if rec.Msg.Answer[0].Header().Ttl == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("table did not become active for %s within %s", remote, timeout)
}

func TestHotReload(t *testing.T) {
	path := writeTable(t, "10.0.0.0/8 100\n")
	p := &TTLRewrite{Next: answerHandler(t)}
	if err := p.loadTable(path); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go p.watch(path, 20*time.Millisecond, stop)

	waitForTTL(t, p, "10.1.1.1", 100, 2*time.Second)

	// Update the table; new requests must follow the new table without a
	// process restart.
	if err := os.WriteFile(path, []byte("10.0.0.0/8 200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForTTL(t, p, "10.1.1.1", 200, 2*time.Second)
}

func TestFailedReloadKeepsPreviousTable(t *testing.T) {
	path := writeTable(t, "10.0.0.0/8 100\n")
	p := &TTLRewrite{Next: answerHandler(t)}
	if err := p.loadTable(path); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go p.watch(path, 20*time.Millisecond, stop)

	broken := []string{
		"10.0.0.0/8 -1\n", // negative ttl
		"10.0.0.0/8\n",    // missing ttl
		"# empty\n",       // empty table must not become active
	}
	for _, table := range broken {
		if err := os.WriteFile(path, []byte(table), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := p.loadTable(path); err == nil {
			t.Fatalf("expected load of %q to fail", table)
		}
		// The previous table must still be active.
		rec := dnstest.NewRecorder(&test.ResponseWriter{RemoteIP: "10.1.1.1"})
		req := new(dns.Msg)
		req.SetQuestion("example.org.", dns.TypeA)
		if _, err := p.ServeDNS(context.TODO(), rec, req); err != nil {
			t.Fatal(err)
		}
		if got := rec.Msg.Answer[0].Header().Ttl; got != 100 {
			t.Fatalf("after failed reload of %q: expected ttl 100 from previous table, got %d", table, got)
		}
	}
}
