package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/inhuman/mcp-ssh-fleet/internal/checks"
	"github.com/inhuman/mcp-ssh-fleet/internal/inventory"
	"github.com/inhuman/mcp-ssh-fleet/internal/sshx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

type ProbeInput struct {
	Host  string   `json:"host,omitempty" jsonschema:"target a SINGLE inventory host by name or address; use this to check one host. Mutually exclusive with tags"`
	Tags  []string `json:"tags,omitempty" jsonschema:"target MULTIPLE hosts by tags; AND semantics — a host matches only if it carries ALL listed tags (like GitLab runner tags). Use host for a single machine instead"`
	Check string   `json:"check" jsonschema:"name of a curated read-only diagnostic to run"`
}

type ProbeOutput struct {
	Check        string       `json:"check"`
	MatchedHosts int          `json:"matched_hosts"`
	Results      []HostResult `json:"results"`
}

// Probe — инструмент ssh_probe: одна курируемая проверка на всех хостах, несущих
// указанные теги (AND), с ограниченным параллелизмом. Класс read-only.
type Probe struct {
	inv         *inventory.Inventory
	ssh         *sshx.Client
	concurrency int
	maxHosts    int
	log         *zap.Logger
}

func NewProbe(inv *inventory.Inventory, client *sshx.Client, concurrency, maxHosts int, log *zap.Logger) *Probe {
	return &Probe{inv: inv, ssh: client, concurrency: concurrency, maxHosts: maxHosts, log: log}
}

func (p *Probe) Register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "ssh_probe", Description: p.description()}, p.handle)
}

func (p *Probe) description() string {
	return "Run one curated read-only diagnostic on inventory host(s). Target EITHER a single host " +
		"(host = inventory name or address) OR a group (tags, AND semantics like GitLab runner tags). " +
		"This is the preferred way to check how a host is doing (uptime, disk, memory, failed services, " +
		"logs) — read-only, you pick a check by name, no arbitrary command. Available checks: " +
		strings.Join(checks.Names(), ", ") + "."
}

func (p *Probe) handle(ctx context.Context, _ *mcp.CallToolRequest, in ProbeInput) (*mcp.CallToolResult, ProbeOutput, error) {
	out, err := p.Execute(ctx, in)
	if err != nil {
		return nil, ProbeOutput{}, err
	}
	return nil, out, nil
}

func (p *Probe) Execute(ctx context.Context, in ProbeInput) (ProbeOutput, error) {
	chk, err := checks.Resolve(in.Check)
	if err != nil {
		return ProbeOutput{}, err
	}

	host := strings.TrimSpace(in.Host)
	if host != "" {
		// Single-host mode: resolve one inventory host (fail-closed).
		h, ok := p.inv.Resolve(host)
		if !ok {
			return ProbeOutput{}, fmt.Errorf("host %q not in inventory (fail-closed): refusing to connect", in.Host)
		}
		res := toHostResult(h, p.ssh.Run(ctx, sshx.Target{Addr: h.Addr, User: h.User, Port: h.Port}, chk.Script))
		p.log.Info("ssh_probe completed", zap.String("check", chk.Name), zap.String("host", h.Name))
		return ProbeOutput{Check: chk.Name, MatchedHosts: 1, Results: []HostResult{res}}, nil
	}

	if !hasNonEmpty(in.Tags) {
		return ProbeOutput{}, fmt.Errorf("specify a target: either host (one machine by name/address) or tags (a group)")
	}

	hosts := p.inv.Match(in.Tags)
	if len(hosts) == 0 {
		return ProbeOutput{Check: chk.Name, MatchedHosts: 0, Results: []HostResult{}}, nil
	}
	if len(hosts) > p.maxHosts {
		return ProbeOutput{}, fmt.Errorf("tags matched %d hosts, exceeds safeguard %d; narrow the tags", len(hosts), p.maxHosts)
	}

	results := make([]HostResult, len(hosts))
	sem := make(chan struct{}, p.concurrency)
	var wg sync.WaitGroup
	for i := range hosts {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			h := hosts[i]
			results[i] = toHostResult(h, p.ssh.Run(ctx, sshx.Target{Addr: h.Addr, User: h.User, Port: h.Port}, chk.Script))
		})
	}
	wg.Wait()

	// Данные хостов не логируем — только метаданные.
	p.log.Info("ssh_probe completed", zap.String("check", chk.Name), zap.Int("matched_hosts", len(hosts)))
	return ProbeOutput{Check: chk.Name, MatchedHosts: len(hosts), Results: results}, nil
}

func hasNonEmpty(tags []string) bool {
	for _, t := range tags {
		if strings.TrimSpace(t) != "" {
			return true
		}
	}
	return false
}
