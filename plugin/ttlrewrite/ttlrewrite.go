// Package ttlrewrite rewrites the TTL of positive DNS answers based on the
// client source network, using a longest-prefix-match subnet table.
package ttlrewrite

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

var log = clog.NewWithPlugin("ttlrewrite")

// TTLRewrite is the plugin handler. It sits in the normal plugin chain and
// post-processes answers produced by the rest of the chain; it never resolves
// anything on its own.
type TTLRewrite struct {
	Next plugin.Handler

	// rules holds the active *ruleSet. Reloads swap it atomically so
	// in-flight requests always see one complete table, never a mix.
	rules atomic.Value
}

// Name implements the plugin.Handler interface.
func (t *TTLRewrite) Name() string { return "ttlrewrite" }

// activeRules returns the currently active rule set, or nil when no table
// has been loaded successfully yet.
func (t *TTLRewrite) activeRules() *ruleSet {
	if v := t.rules.Load(); v != nil {
		return v.(*ruleSet)
	}
	return nil
}

// ServeDNS implements the plugin.Handler interface.
func (t *TTLRewrite) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	rw := &responseWriter{
		ResponseWriter: w,
		state:          state,
		rules:          t.activeRules(),
	}
	return plugin.NextOrFailure(t.Name(), t.Next, ctx, rw, r)
}

// responseWriter intercepts the answer on its way out and rewrites TTLs on
// positive responses only.
type responseWriter struct {
	dns.ResponseWriter
	state request.Request
	rules *ruleSet
}

// WriteMsg rewrites the TTL of every record in the answer section when the
// response is a positive answer (NOERROR with records) and the client source
// address matches a rule. Everything else is passed through untouched: rcode,
// answer content, and empty or negative responses are never modified.
func (rw *responseWriter) WriteMsg(m *dns.Msg) error {
	if m == nil {
		return fmt.Errorf("ttlrewrite: cannot process nil message")
	}
	if m.Rcode != dns.RcodeSuccess || len(m.Answer) == 0 {
		return rw.ResponseWriter.WriteMsg(m)
	}
	ip := net.ParseIP(rw.state.IP())
	if ip == nil {
		return fmt.Errorf("ttlrewrite: cannot determine client address %q, aborting rewrite", rw.state.IP())
	}
	rule := rw.rules.match(ip)
	if rule == nil {
		return rw.ResponseWriter.WriteMsg(m)
	}
	// Rewrite a copy so the original message is never mutated; other
	// handlers or caches may still hold references to it.
	out := m.Copy()
	if out == nil {
		return fmt.Errorf("ttlrewrite: failed to copy message for %s, aborting rewrite", rw.state.Name())
	}
	for _, rr := range out.Answer {
		if rr == nil || rr.Header() == nil {
			return fmt.Errorf("ttlrewrite: malformed answer record for %s, aborting rewrite", rw.state.Name())
		}
		old := rr.Header().Ttl
		rr.Header().Ttl = rule.TTL
		log.Infof("client %s qname %s matched %s: ttl %d -> %d", rw.state.IP(), rw.state.Name(), rule.Network.String(), old, rule.TTL)
	}
	return rw.ResponseWriter.WriteMsg(out)
}
