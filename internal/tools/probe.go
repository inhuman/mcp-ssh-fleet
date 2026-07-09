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
	Tags  []string `json:"tags" jsonschema:"tags to target hosts by; AND semantics — a host matches only if it carries ALL listed tags (like GitLab runner tags); must be non-empty"`
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
	return "Run one curated read-only diagnostic on all inventory hosts carrying the given tags " +
		"(AND semantics, like GitLab runner tags). Read-only: you pick a check by name, you cannot pass " +
		"an arbitrary command. Available checks: " + strings.Join(checks.Names(), ", ") + "."
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
	if !hasNonEmpty(in.Tags) {
		return ProbeOutput{}, fmt.Errorf("tags must be non-empty: specify which hosts to probe")
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
