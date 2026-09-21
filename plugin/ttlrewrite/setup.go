package ttlrewrite

import (
	"fmt"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() { plugin.Register("ttlrewrite", setup) }

const defaultReloadInterval = time.Second

func setup(c *caddy.Controller) error {
	path, interval, err := parse(c)
	if err != nil {
		return plugin.Error("ttlrewrite", err)
	}

	t := &TTLRewrite{}
	// The table must load at startup; a missing or invalid table fails the
	// whole setup instead of silently running without rules.
	if err := t.loadTable(path); err != nil {
		return plugin.Error("ttlrewrite", err)
	}

	stop := make(chan struct{})
	c.OnShutdown(func() error {
		close(stop)
		return nil
	})
	go t.watch(path, interval, stop)

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		t.Next = next
		return t
	})
	return nil
}

// parse reads the Corefile stanza:
//
//	ttlrewrite FILE {
//	    reload 5s
//	}
func parse(c *caddy.Controller) (string, time.Duration, error) {
	var path string
	interval := defaultReloadInterval
	for c.Next() {
		args := c.RemainingArgs()
		if len(args) != 1 {
			return "", 0, c.ArgErr()
		}
		if path != "" {
			return "", 0, fmt.Errorf("only one rule table file may be specified")
		}
		path = args[0]
		for c.NextBlock() {
			switch c.Val() {
			case "reload":
				vals := c.RemainingArgs()
				if len(vals) != 1 {
					return "", 0, c.ArgErr()
				}
				d, err := time.ParseDuration(vals[0])
				if err != nil {
					return "", 0, fmt.Errorf("invalid reload interval %q: %w", vals[0], err)
				}
				if d <= 0 {
					return "", 0, fmt.Errorf("reload interval must be positive, got %s", d)
				}
				interval = d
			default:
				return "", 0, fmt.Errorf("unknown option %q", c.Val())
			}
		}
	}
	if path == "" {
		return "", 0, fmt.Errorf("no rule table file specified")
	}
	return path, interval, nil
}
