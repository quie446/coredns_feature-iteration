package ttlrewrite

import (
	"fmt"
	"os"
	"time"
)

// loadTable reads and validates the rule table from path. The new table only
// becomes active after the whole file parses; any validation error aborts the
// swap and the previous table keeps serving.
func (t *TTLRewrite) loadTable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open rule table %s: %w", path, err)
	}
	defer f.Close()
	rs, err := parseRules(f)
	if err != nil {
		return fmt.Errorf("invalid rule table %s: %w", path, err)
	}
	t.rules.Store(rs)
	return nil
}

// watch polls the rule table file and hot-reloads it whenever it changes, so
// an updated table takes effect without restarting the process. A failed
// reload logs the exact reason and keeps the previous table; an empty table
// is rejected and never becomes active.
func (t *TTLRewrite) watch(path string, interval time.Duration, stop <-chan struct{}) {
	var lastMod time.Time
	var lastSize int64 = -1
	if info, err := os.Stat(path); err == nil {
		lastMod = info.ModTime()
		lastSize = info.Size()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil {
				log.Errorf("reload of %s skipped: %v", path, err)
				continue
			}
			if info.ModTime().Equal(lastMod) && info.Size() == lastSize {
				continue
			}
			lastMod = info.ModTime()
			lastSize = info.Size()
			if err := t.loadTable(path); err != nil {
				log.Errorf("reload of %s failed, keeping previous table: %v", path, err)
				continue
			}
			log.Infof("reloaded %s: %d rule(s) now active", path, t.activeRules().len())
		}
	}
}
