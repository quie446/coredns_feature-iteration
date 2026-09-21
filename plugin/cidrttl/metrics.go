package cidrttl

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const pluginName = "cidrttl"

var (
	// HitCount records every rewritten record. The matched network, source
	// address and the old/new TTL are carried as labels so a single observation
	// answers: which network rule fired, from which source, and how the TTL
	// changed. Different sources therefore show up as distinct series.
	HitCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "ttl_rewrite_hits_total",
		Help:      "Counter of answer records whose TTL was rewritten by client network.",
	}, []string{"server", "view", "network", "source", "old_ttl", "new_ttl"})

	// MissCount records positive answers from sources that matched no network
	// and were forwarded unchanged.
	MissCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "ttl_rewrite_misses_total",
		Help:      "Counter of positive answers not rewritten because the client network matched no entry.",
	}, []string{"server", "view"})

	// RewriteFailureCount records responses that were aborted because rewriting
	// could not be completed.
	RewriteFailureCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "ttl_rewrite_failures_total",
		Help:      "Counter of responses aborted due to a TTL rewrite failure.",
	}, []string{"server", "view"})

	// ReloadSuccessCount / ReloadFailureCount make hot reload outcomes visible.
	// Every failed reload is counted with its reason, so a silent failure can
	// never masquerade as a successful reload.
	ReloadSuccessCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "table_reload_success_total",
		Help:      "Counter of successful mapping table reloads.",
	}, []string{"server", "view"})

	ReloadFailureCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "table_reload_failure_total",
		Help:      "Counter of failed mapping table reloads, labeled with the reason.",
	}, []string{"server", "view", "reason"})
)
