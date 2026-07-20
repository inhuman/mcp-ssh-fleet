// Package checks is the built-in registry of curated read-only diagnostics for ssh_probe.
// The model picks a check by name only; the script body is defined here and never comes
// from the arguments (injection-proof and deterministic).
package checks

import (
	"fmt"
	"slices"
	"strings"
)

type Check struct {
	Name        string
	Script      string
	Description string
}

// registry is the set of available checks. Every script is non-interactive and read-only.
var registry = map[string]Check{
	"uptime": {"uptime", "uptime", "uptime and load average"},
	"disk":   {"disk", "df -h", "free disk space"},
	"mem":    {"mem", "free -m", "memory usage"},
	"failed": {"failed", "systemctl --failed --no-legend --no-pager 2>/dev/null || echo 'systemctl unavailable'", "failed systemd services"},
	"logs":   {"logs", "journalctl -n 50 --no-pager 2>/dev/null || tail -n 50 /var/log/syslog 2>/dev/null || echo 'no system log'", "tail of the system log"},
}

func Resolve(name string) (Check, error) {
	c, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Check{}, fmt.Errorf("unknown check %q; available: %s", name, strings.Join(Names(), ", "))
	}
	return c, nil
}

// Names returns the sorted list of check names (used in the tool description and errors).
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}
