package ttlrewrite

import (
	"context"
	"strings"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// stubHandler answers A queries for example.org. with a fixed TTL of 600.
type stubHandler struct {
	rcode  int
	answer []dns.RR
}

func (s stubHandler) Name() string { return "stub" }

func (s stubHandler) ServeDNS(_ context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Rcode = s.rcode
	m.Answer = s.answer
	if err := w.WriteMsg(m); err != nil {
		return m.Rcode, err
	}
	return m.Rcode, nil
}

func answerHandler(t *testing.T) stubHandler {
	t.Helper()
	rr, err := dns.NewRR("example.org. 600 IN A 192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	return stubHandler{rcode: dns.RcodeSuccess, answer: []dns.RR{rr}}
}

func newTestPlugin(t *testing.T, table string, next stubHandler) *TTLRewrite {
	t.Helper()
	rs, err := parseRules(strings.NewReader(table))
	if err != nil {
		t.Fatal(err)
	}
	p := &TTLRewrite{Next: next}
	p.rules.Store(rs)
	return p
}

func query(t *testing.T, p *TTLRewrite, remoteAddr string) *dnstest.Recorder {
	t.Helper()
	rec := dnstest.NewRecorder(&test.ResponseWriter{RemoteIP: remoteAddr})
	req := new(dns.Msg)
	req.SetQuestion("example.org.", dns.TypeA)
	if _, err := p.ServeDNS(context.TODO(), rec, req); err != nil {
		t.Fatalf("ServeDNS failed: %v", err)
	}
	return rec
}

func TestRewriteMatchedClient(t *testing.T) {
	p := newTestPlugin(t, "10.0.0.0/8 42\n", answerHandler(t))
	rec := query(t, p, "10.1.2.3")
	if len(rec.Msg.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(rec.Msg.Answer))
	}
	if rec.Msg.Answer[0].Header().Ttl != 42 {
		t.Fatalf("expected ttl 42, got %d", rec.Msg.Answer[0].Header().Ttl)
	}
	// Answer content must be untouched.
	a, ok := rec.Msg.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("answer type changed: %T", rec.Msg.Answer[0])
	}
	if a.A.String() != "192.0.2.1" || a.Hdr.Name != "example.org." {
		t.Fatalf("answer content changed: %s", rec.Msg.Answer[0].String())
	}
}

func TestUnmatchedClientPassesThrough(t *testing.T) {
	p := newTestPlugin(t, "10.0.0.0/8 42\n", answerHandler(t))
	rec := query(t, p, "192.0.2.9")
	if rec.Msg.Answer[0].Header().Ttl != 600 {
		t.Fatalf("expected original ttl 600, got %d", rec.Msg.Answer[0].Header().Ttl)
	}
}

func TestNegativeResponseUntouched(t *testing.T) {
	p := newTestPlugin(t, "10.0.0.0/8 42\n", stubHandler{rcode: dns.RcodeNameError})
	rec := query(t, p, "10.1.2.3")
	if rec.Msg.Rcode != dns.RcodeNameError {
		t.Fatalf("rcode changed: %d", rec.Msg.Rcode)
	}
	if len(rec.Msg.Answer) != 0 {
		t.Fatalf("empty answer section was modified")
	}
}

func TestNodataResponseUntouched(t *testing.T) {
	p := newTestPlugin(t, "10.0.0.0/8 42\n", stubHandler{rcode: dns.RcodeSuccess})
	rec := query(t, p, "10.1.2.3")
	if len(rec.Msg.Answer) != 0 {
		t.Fatalf("NODATA response gained answers")
	}
}

func TestDifferentSourcesGetDifferentTTLs(t *testing.T) {
	p := newTestPlugin(t, "10.0.0.0/8 10\n192.168.0.0/16 20\n", answerHandler(t))
	if got := query(t, p, "10.9.9.9").Msg.Answer[0].Header().Ttl; got != 10 {
		t.Fatalf("10/8 client: expected ttl 10, got %d", got)
	}
	if got := query(t, p, "192.168.1.1").Msg.Answer[0].Header().Ttl; got != 20 {
		t.Fatalf("192.168/16 client: expected ttl 20, got %d", got)
	}
	if got := query(t, p, "203.0.113.1").Msg.Answer[0].Header().Ttl; got != 600 {
		t.Fatalf("unmatched client: expected ttl 600, got %d", got)
	}
}

func TestStateIPParsing(t *testing.T) {
	state := request.Request{W: &test.ResponseWriter{RemoteIP: "10.1.2.3"}}
	if state.IP() != "10.1.2.3" {
		t.Fatalf("unexpected ip %q", state.IP())
	}
}
