package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/inhuman/mcp-ssh-fleet/internal/inventory"
	"github.com/inhuman/mcp-ssh-fleet/internal/sshx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

type ExecInput struct {
	Host    string `json:"host" jsonschema:"inventory host name or address to run on; MUST be present in the server inventory (fail-closed)"`
	Command string `json:"command" jsonschema:"a single non-interactive shell command to run on the host"`
}

// Exec is the ssh_exec tool: one arbitrary command on ONE inventory host.
// Class write-external; the server runs the command — access gating is the MCP client's job.
type Exec struct {
	inv *inventory.Inventory
	ssh *sshx.Client
	log *zap.Logger
}

func NewExec(inv *inventory.Inventory, client *sshx.Client, log *zap.Logger) *Exec {
	return &Exec{inv: inv, ssh: client, log: log}
}

func (e *Exec) Register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "ssh_exec",
		Description: "Run a single arbitrary non-interactive command on ONE inventory host and return its output. " +
			"The host MUST exist in the server inventory (fail-closed; arbitrary addresses are refused). " +
			"Destructive: the MCP client is expected to gate this behind approval/RBAC.",
	}, e.handle)
}

func (e *Exec) handle(ctx context.Context, _ *mcp.CallToolRequest, in ExecInput) (*mcp.CallToolResult, HostResult, error) {
	out, err := e.Execute(ctx, in)
	if err != nil {
		return nil, HostResult{}, err
	}
	return nil, out, nil
}

func (e *Exec) Execute(ctx context.Context, in ExecInput) (HostResult, error) {
	if strings.TrimSpace(in.Command) == "" {
		return HostResult{}, fmt.Errorf("command must not be empty")
	}
	h, ok := e.inv.Resolve(in.Host)
	if !ok {
		return HostResult{}, fmt.Errorf("host %q not in inventory (fail-closed): refusing to connect", in.Host)
	}
	res := toHostResult(h, e.ssh.Run(ctx, sshx.Target{Addr: h.Addr, User: h.User, Port: h.Port}, in.Command))
	e.log.Info("ssh_exec completed", zap.String("host", h.Name), zap.String("status", res.Status))
	return res, nil
}
