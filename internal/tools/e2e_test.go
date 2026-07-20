package tools

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/inhuman/mcp-ssh-fleet/internal/inventory"
	"github.com/inhuman/mcp-ssh-fleet/internal/sshx"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

// e2e: a real in-process SSH server (genuine x/crypto/ssh handshake plus command execution
// over the wire), NOT a mock. Exercises sshx + tools end to end.

func TestProbeExec_E2E(t *testing.T) {
	portA := startSSHServer(t, shellExec)
	portB := startSSHServer(t, shellExec)

	invYAML := fmt.Sprintf(`
hosts:
  - {name: vm-a, addr: 127.0.0.1, user: tester, port: %d, tags: [stand-a, test]}
  - {name: vm-b, addr: 127.0.0.1, user: tester, port: %d, tags: [stand-b, test]}
  - {name: vm-dead, addr: 127.0.0.1, user: tester, port: 1, tags: [stand-a]}
`, portA, portB)
	inv, err := inventory.Parse([]byte(invYAML))
	if err != nil {
		t.Fatal(err)
	}
	client := newClient(t, 3*time.Second)
	log := zap.NewNop()

	probe := NewProbe(inv, client, 8, 50, log)
	execTool := NewExec(inv, client, log)
	ctx := t.Context()

	// probe by tag 'test' hits vm-a + vm-b, both ok, with real uptime output.
	out, err := probe.Execute(ctx, ProbeInput{Tags: []string{"test"}, Check: "uptime"})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.MatchedHosts != 2 || len(out.Results) != 2 {
		t.Fatalf("want 2 matched, got %d (%+v)", out.MatchedHosts, out.Results)
	}
	for _, r := range out.Results {
		if r.Status != string(sshx.StatusOK) {
			t.Fatalf("host %s status=%s reason=%s", r.Host, r.Status, r.Reason)
		}
		if r.Output == "" || r.Fingerprint == "" {
			t.Fatalf("host %s empty output/fingerprint", r.Host)
		}
	}

	// partial failure: tag stand-a hits vm-a (ok) + vm-dead (port 1, unreachable/timeout).
	out, err = probe.Execute(ctx, ProbeInput{Tags: []string{"stand-a"}, Check: "disk"})
	if err != nil {
		t.Fatalf("probe stand-a: %v", err)
	}
	byName := map[string]HostResult{}
	for _, r := range out.Results {
		byName[r.Host] = r
	}
	if byName["vm-a"].Status != string(sshx.StatusOK) {
		t.Fatalf("vm-a should be ok, got %s", byName["vm-a"].Status)
	}
	if s := byName["vm-dead"].Status; s == string(sshx.StatusOK) {
		t.Fatalf("vm-dead should have failed, got %s", s)
	}

	// no hosts match the tags: an honest empty result, not an error.
	out, err = probe.Execute(ctx, ProbeInput{Tags: []string{"nope"}, Check: "mem"})
	if err != nil || out.MatchedHosts != 0 {
		t.Fatalf("no-match: err=%v matched=%d", err, out.MatchedHosts)
	}

	// empty tags are refused (never fan out across the whole fleet).
	if _, err := probe.Execute(ctx, ProbeInput{Tags: nil, Check: "mem"}); err == nil {
		t.Fatal("empty tags must be rejected")
	}

	// probing a single host by name yields one section, ok.
	single, err := probe.Execute(ctx, ProbeInput{Host: "vm-a", Check: "uptime"})
	if err != nil {
		t.Fatalf("probe host: %v", err)
	}
	if single.MatchedHosts != 1 || len(single.Results) != 1 || single.Results[0].Host != "vm-a" || single.Results[0].Status != string(sshx.StatusOK) {
		t.Fatalf("single-host probe bad result: %+v", single)
	}
	// probing a single host by address resolves too.
	if byAddr, err := probe.Execute(ctx, ProbeInput{Host: "127.0.0.1", Check: "uptime"}); err != nil || byAddr.MatchedHosts != 1 {
		t.Fatalf("probe by addr: err=%v matched=%d", err, byAddr.MatchedHosts)
	}
	// probing a host outside the inventory is refused (fail-closed).
	if _, err := probe.Execute(ctx, ProbeInput{Host: "10.9.9.9", Check: "uptime"}); err == nil {
		t.Fatal("probe host outside inventory must fail-closed")
	}
	// neither host nor tags is an error.
	if _, err := probe.Execute(ctx, ProbeInput{Check: "uptime"}); err == nil {
		t.Fatal("probe without host or tags must be rejected")
	}

	// exec of an arbitrary command on an inventory host.
	er, err := execTool.Execute(ctx, ExecInput{Host: "vm-a", Command: "echo hello-fleet"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if er.Status != string(sshx.StatusOK) || er.Output == "" {
		t.Fatalf("exec bad result: %+v", er)
	}

	// exec against a host outside the inventory refuses to connect (fail-closed).
	if _, err := execTool.Execute(ctx, ExecInput{Host: "10.9.9.9", Command: "id"}); err == nil {
		t.Fatal("host outside inventory must fail-closed")
	}

	// the private key never leaks into output or reason.
	for _, r := range out.Results {
		if containsKeyMaterial(r.Output) || containsKeyMaterial(r.Reason) {
			t.Fatal("key material leaked into output")
		}
	}
}

func TestExec_Timeout(t *testing.T) {
	port := startSSHServer(t, shellExec)
	inv, err := inventory.Parse([]byte(fmt.Sprintf(
		"hosts:\n  - {name: slow, addr: 127.0.0.1, user: tester, port: %d, tags: [t]}\n", port)))
	if err != nil {
		t.Fatal(err)
	}
	client := newClient(t, 1*time.Second) // short timeout
	er, err := NewExec(inv, client, zap.NewNop()).Execute(t.Context(), ExecInput{Host: "slow", Command: "sleep 3"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if er.Status != string(sshx.StatusTimeout) {
		t.Fatalf("want timeout, got %s (%s)", er.Status, er.Reason)
	}
}

func TestProbe_OutputCap(t *testing.T) {
	port := startSSHServer(t, shellExec)
	inv, _ := inventory.Parse([]byte(fmt.Sprintf(
		"hosts:\n  - {name: big, addr: 127.0.0.1, user: tester, port: %d, tags: [t]}\n", port)))
	// small cap
	client := newClientCap(t, 3*time.Second, 64)
	out, err := NewProbe(inv, client, 4, 50, zap.NewNop()).
		Execute(t.Context(), ProbeInput{Tags: []string{"t"}, Check: "uptime"})
	if err != nil {
		t.Fatal(err)
	}
	_ = out
	// prints a large blob
	er, err := NewExec(inv, client, zap.NewNop()).
		Execute(t.Context(), ExecInput{Host: "big", Command: "for i in $(seq 1 200); do echo XXXXXXXXXX; done"})
	if err != nil {
		t.Fatal(err)
	}
	if !er.Truncated || len(er.Output) > 200 {
		t.Fatalf("output should be capped/truncated, got trunc=%v len=%d", er.Truncated, len(er.Output))
	}
}

// --- real in-process SSH server (helper) ---

func startSSHServer(t *testing.T, run func(cmd string) (string, int)) int {
	t.Helper()
	hostSigner := genSigner(t)
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConn(conn, cfg, run)
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return port
}

func serveConn(conn net.Conn, cfg *ssh.ServerConfig, run func(cmd string) (string, int)) {
	sc, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer func() { _ = sc.Close() }()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, requests, err := nc.Accept()
		if err != nil {
			return
		}
		go handleSession(ch, requests, run)
	}
}

func handleSession(ch ssh.Channel, requests <-chan *ssh.Request, run func(cmd string) (string, int)) {
	for req := range requests {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)
		out, code := run(execPayload(req.Payload))
		_, _ = ch.Write([]byte(out))
		_, _ = ch.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, uint32(code)))
		_ = ch.Close()
		return
	}
}

func execPayload(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	n := binary.BigEndian.Uint32(payload)
	if int(n) > len(payload)-4 {
		return ""
	}
	return string(payload[4 : 4+n])
}

func shellExec(cmd string) (string, int) {
	out, err := exec.CommandContext(context.Background(), "sh", "-c", cmd).CombinedOutput()
	code := 0
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}

func genSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newClient(t *testing.T, timeout time.Duration) *sshx.Client {
	return newClientCap(t, timeout, 8192)
}

func newClientCap(t *testing.T, timeout time.Duration, limit int) *sshx.Client {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o400); err != nil {
		t.Fatal(err)
	}
	c, err := sshx.New(path, limit, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func containsKeyMaterial(s string) bool {
	return len(s) > 0 && (contains(s, "PRIVATE KEY") || contains(s, "OPENSSH PRIVATE"))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
