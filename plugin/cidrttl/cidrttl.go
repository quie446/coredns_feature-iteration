package cidrttl

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

var log = clog.NewWithPlugin("cidrttl")

// CidrTTL rewrites the TTL of positive answers based on the client source IP.
// It does not run its own resolver: it only inspects and adjusts responses
// produced by the rest of the plugin chain.
type CidrTTL struct {
	Next plugin.Handler

	path string

	// current is the active immutable table snapshot. It is swapped atomically
	// on successful reload, so failed reloads keep serving the previous table.
	current atomic.Pointer[table]

	stop chan struct{}
	wg   sync.WaitGroup
}

// ServeDNS implements plugin.Handler.
func (c *CidrTTL) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	ww := &ttlWriter{
		ResponseWriter: w,
		state:          request.Request{W: w, Req: r},
		cidrttl:        c,
		ctx:            ctx,
	}
	return plugin.NextOrFailure(c.Name(), c.Next, ctx, ww, r)
}

// Name implements plugin.Handler.
func (c *CidrTTL) Name() string { return "cidrttl" }

type ttlWriter struct {
	dns.ResponseWriter
	state   request.Request
	cidrttl *CidrTTL
	ctx     context.Context
}

func (w *ttlWriter) WriteMsg(m *dns.Msg) error {
	// Only positive, non-empty responses are eligible. Negative answers
	// (NXDOMAIN/SERVFAIL/...) and no-data responses pass through untouched, so a
	// name without records can never gain any.
	if m != nil && m.Rcode == dns.RcodeSuccess && len(m.Answer) > 0 {
		if err := w.rewrite(m); err != nil {
			// Do not write a half-rewritten message: surface the failure and stop
			// the whole response instead of pretending the rewrite completed.
			RewriteFailureCount.WithLabelValues(metrics.WithServer(w.ctx), metrics.WithView(w.ctx)).Inc()
			log.Errorf("aborting response for %q: %v", w.state.Name(), err)
			return err
		}
	}
	return w.ResponseWriter.WriteMsg(m)
}

func (w *ttlWriter) rewrite(m *dns.Msg) error {
	ip := parseSourceIP(w.state.IP())
	if ip == nil {
		// Unparseable peer address: nothing to match against, pass through.
		return nil
	}

	t := w.cidrttl.current.Load()
	if t == nil {
		// No table ever loaded successfully; do not invent matches.
		return nil
	}

	entry, ok := t.lookup(ip)
	if !ok {
		// Source outside every configured network: forward unchanged.
		MissCount.WithLabelValues(metrics.WithServer(w.ctx), metrics.WithView(w.ctx)).Inc()
		return nil
	}

	for _, rr := range m.Answer {
		hdr := rr.Header()
		oldTTL := hdr.Ttl
		hdr.Ttl = entry.ttl
		HitCount.WithLabelValues(
			metrics.WithServer(w.ctx),
			metrics.WithView(w.ctx),
			entry.rawText,
			w.state.IP(),
			strconv.FormatUint(uint64(oldTTL), 10),
			strconv.FormatUint(uint64(entry.ttl), 10),
		).Inc()
	}
	return nil
}

func parseSourceIP(addr string) net.IP {
	if idx := strings.IndexByte(addr, '%'); idx >= 0 {
		addr = addr[:idx]
	}
	return net.ParseIP(addr)
}
