package cidrttl

import (
	"fmt"
	"os"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() { plugin.Register(pluginName, setup) }

const defaultReload = 30 * time.Second

func setup(c *caddy.Controller) error {
	ct, reload, err := parseConfig(c)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	// The initial load happens before the server starts serving. A bad table
	// (missing file, invalid row, empty table) prevents startup instead of
	// running with an unconfigured rewrite stage.
	if err := ct.load("initial load"); err != nil {
		return plugin.Error(pluginName, err)
	}

	if reload > 0 {
		ct.startReloader(reload)
		c.OnFinalShutdown(func() error {
			ct.stopReloader()
			return nil
		})
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		ct.Next = next
		return ct
	})

	return nil
}

func parseConfig(c *caddy.Controller) (*CidrTTL, time.Duration, error) {
	ct := &CidrTTL{stop: make(chan struct{})}
	var reload = defaultReload

	for c.Next() {
		args := c.RemainingArgs()
		if len(args) != 1 {
			return nil, 0, c.ArgErr()
		}
		ct.path = args[0]

		for c.NextBlock() {
			switch c.Val() {
			case "reload":
				rem := c.RemainingArgs()
				if len(rem) != 1 {
					return nil, 0, c.Errf("reload needs a duration (use 0 to disable)")
				}
				d, err := time.ParseDuration(rem[0])
				if err != nil {
					return nil, 0, c.Errf("invalid reload duration %q: %v", rem[0], err)
				}
				if d < 0 {
					return nil, 0, c.Errf("reload duration must not be negative: %s", rem[0])
				}
				reload = d
			default:
				return nil, 0, c.Errf("unknown property %q", c.Val())
			}
		}
	}
	return ct, reload, nil
}

// load reads and validates the table file and atomically swaps it in. On any
// failure the previously active table is left untouched and the reason is
// logged and counted; the error is returned so initial startup aborts.
func (c *CidrTTL) load(stage string) error {
	f, err := os.Open(c.path)
	if err != nil {
		return c.reportFailure(fmt.Errorf("%s: cannot open table file %q: %v", stage, c.path, err))
	}
	defer f.Close()

	t, err := parseTable(f)
	if err != nil {
		return c.reportFailure(fmt.Errorf("%s: %v", stage, err))
	}

	c.current.Store(t)
	log.Infof("%s: mapping table %q loaded with %d entries", stage, c.path, len(t.v4)+len(t.v6))
	return nil
}

func (c *CidrTTL) reportFailure(err error) error {
	log.Error(err)
	ReloadFailureCount.WithLabelValues("", "", err.Error()).Inc()
	return err
}

func (c *CidrTTL) startReloader(interval time.Duration) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-ticker.C:
				if err := c.load("reload"); err != nil {
					// Reason already logged/counted in load; the old table stays.
					continue
				}
				ReloadSuccessCount.WithLabelValues("", "").Inc()
			}
		}
	}()
}

func (c *CidrTTL) stopReloader() {
	close(c.stop)
	c.wg.Wait()
}
