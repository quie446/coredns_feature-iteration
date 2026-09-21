package cidrttl

import (
	"context"
	"net"
	"testing"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// answerHandler replies with a fixed message so the rewrite layer can be
// exercised against realistic upstream output.
type answerHandler struct {
	answer *dns.Msg
}

func (h answerHandler) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	w.WriteMsg(h.answer)
	return dns.RcodeSuccess, nil
}
func (h answerHandler) Name() string { return "answerHandler" }

func newPlugin(t *testing.T, tableContent string) *CidrTTL {
	t.Helper()
	tab := mustTable(t, tableContent)
	c := &CidrTTL{Next: nil}
	c.current.Store(tab)
	return c
}

func serve(t *testing.T, c *CidrTTL, remoteIP string, reply *dns.Msg) *dns.Msg {
	t.Helper()
	c.Next = answerHandler{answer: reply}

	req := new(dns.Msg)
	req.SetQuestion("example.org.", dns.TypeA)

	rec := dnstest.NewRecorder(&test.ResponseWriter{RemoteIP: remoteIP})
	if _, err := c.ServeDNS(context.Background(), rec, req); err != nil {
		t.Fatalf("ServeDNS returned error: %v", err)
	}
	if rec.Msg == nil {
		t.Fatal("expected a response message")
	}
	return rec.Msg
}

func positiveReply(ttl uint32, rcode int) *dns.Msg {
	m := new(dns.Msg)
	m.SetQuestion("example.org.", dns.TypeA)
	m.Rcode = rcode
	a := new(dns.A)
	a.Hdr = dns.RR_Header{Name: "example.org.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl}
	a.A = net.ParseIP("93.184.216.34").To4()
	m.Answer = []dns.RR{a}
	return m
}

func TestRewritePositiveAnswer(t *testing.T) {
	c := newPlugin(t, "10.0.0.0/8 60\n10.1.0.0/16 30\n")

	m := serve(t, c, "10.1.2.3", positiveReply(300, dns.RcodeSuccess))
	if got := m.Answer[0].Header().Ttl; got != 30 {
		t.Fatalf("expected TTL 30 (more specific /16), got %d", got)
	}

	m = serve(t, c, "10.9.9.9", positiveReply(300, dns.RcodeSuccess))
	if got := m.Answer[0].Header().Ttl; got != 60 {
		t.Fatalf("expected TTL 60 (/8), got %d", got)
	}

	// Answer content other than TTL must be untouched.
	if m.Answer[0].(*dns.A).A.String() != "93.184.216.34" {
		t.Fatal("answer record content was modified")
	}
}

func TestRewriteMissPassThrough(t *testing.T) {
	c := newPlugin(t, "10.0.0.0/8 60\n")
	m := serve(t, c, "192.0.2.5", positiveReply(300, dns.RcodeSuccess))
	if got := m.Answer[0].Header().Ttl; got != 300 {
		t.Fatalf("unmatched request must pass TTL through, got %d", got)
	}
}

func TestRewriteSkipsNoDataAndNegative(t *testing.T) {
	c := newPlugin(t, "10.0.0.0/8 60\n")

	nx := positiveReply(300, dns.RcodeNameError)
	nx.Answer = nil
	m := serve(t, c, "10.0.0.1", nx)
	if m.Rcode != dns.RcodeNameError || len(m.Answer) != 0 {
		t.Fatal("NXDOMAIN must not gain any records")
	}

	nodata := positiveReply(300, dns.RcodeSuccess)
	nodata.Answer = nil
	m = serve(t, c, "10.0.0.1", nodata)
	if len(m.Answer) != 0 {
		t.Fatal("no-data success must not gain any records")
	}
}

func TestHitObservabilityPerSource(t *testing.T) {
	c := newPlugin(t, "10.0.0.0/8 60\n10.1.0.0/16 30\n")

	serve(t, c, "10.9.0.1", positiveReply(300, dns.RcodeSuccess))
	serve(t, c, "10.1.0.1", positiveReply(120, dns.RcodeSuccess))
	serve(t, c, "10.1.0.1", positiveReply(120, dns.RcodeSuccess))

	v := func(network, source, oldTTL string) float64 {
		return testutil.ToFloat64(HitCount.WithLabelValues("", "", network, source, oldTTL, map[string]string{
			"10.9.0.1": "60",
			"10.1.0.1": "30",
		}[source]))
	}
	if got := v("10.0.0.0/8", "10.9.0.1", "300"); got < 1 {
		t.Fatalf("expected hit series for /8 source with old ttl 300, got %v", got)
	}
	if got := v("10.1.0.0/16", "10.1.0.1", "120"); got < 2 {
		t.Fatalf("expected 2 hits for /16 source with old ttl 120, got %v", got)
	}
	// A different source must not increment the other source's series.
	if got := v("10.1.0.0/16", "10.1.0.1", "300"); got != 0 {
		t.Fatalf("observations must not mix sources/old-ttl, got %v", got)
	}
}

var _ plugin.Handler = answerHandler{}
