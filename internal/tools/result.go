package tools

import (
	"github.com/inhuman/mcp-ssh-fleet/internal/inventory"
	"github.com/inhuman/mcp-ssh-fleet/internal/sshx"
)

// HostResult is the per-host section of a response (shared by probe and exec).
type HostResult struct {
	Host        string `json:"host"`
	Addr        string `json:"addr"`
	Status      string `json:"status" jsonschema:"ok | unreachable | timeout | error"`
	Output      string `json:"output,omitempty" jsonschema:"command/check output, truncated to the server cap"`
	Reason      string `json:"reason,omitempty" jsonschema:"failure reason when status is not ok"`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"SHA256 fingerprint of the host key (for audit)"`
	Truncated   bool   `json:"truncated,omitempty"`
}

func toHostResult(h inventory.Host, r sshx.Result) HostResult {
	return HostResult{
		Host:        h.Name,
		Addr:        h.Addr,
		Status:      string(r.Status),
		Output:      r.Output,
		Reason:      r.Reason,
		Fingerprint: r.Fingerprint,
		Truncated:   r.Truncated,
	}
}
