// Package sshx is the SSH client: one connection, one non-interactive command, then close.
// The private key is read from a pod secret and is never serialized into output or logs.
// Host keys are verified TOFU-style (trust on first use, with the fingerprint recorded);
// a mismatch on any later contact rejects the connection.
package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type Status string

const (
	StatusOK          Status = "ok"
	StatusUnreachable Status = "unreachable"
	StatusTimeout     Status = "timeout"
	StatusError       Status = "error"
)

type Target struct {
	Addr string
	User string
	Port int
}

type Result struct {
	Status      Status
	Output      string
	Truncated   bool
	Reason      string
	Fingerprint string
}

type Client struct {
	signer   ssh.Signer
	capBytes int
	timeout  time.Duration
	known    *fingerprintStore
}

// New reads the private key from keyPath (a pod secret) and builds the client. A missing or
// malformed key is a startup error — the server must not silently accept calls that are all
// going to fail.
func New(keyPath string, capBytes int, timeout time.Duration) (*Client, error) {
	pem, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %q: %w", keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return &Client{
		signer:   signer,
		capBytes: capBytes,
		timeout:  timeout,
		known:    &fingerprintStore{m: make(map[string]string)},
	}, nil
}

// Run connects to the target and executes one non-interactive command. Operational failures
// (unreachable, timeout, host-key mismatch) are encoded in Result.Status rather than returned
// as an error.
func (c *Client) Run(ctx context.Context, t Target, command string) Result {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	hostKey := net.JoinHostPort(t.Addr, strconv.Itoa(t.Port))
	holder := &fpHolder{}
	cfg := &ssh.ClientConfig{
		User:            t.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(c.signer)},
		HostKeyCallback: c.hostKeyCallback(hostKey, holder),
		Timeout:         c.timeout,
	}

	client, res, ok := c.dial(ctx, hostKey, cfg, holder)
	if !ok {
		return res
	}
	defer func() { _ = client.Close() }()

	return c.runOne(ctx, client, command, holder)
}

func (c *Client) dial(ctx context.Context, addr string, cfg *ssh.ClientConfig, holder *fpHolder) (*ssh.Client, Result, bool) {
	type dialRes struct {
		cl  *ssh.Client
		err error
	}
	ch := make(chan dialRes, 1)
	go func() {
		cl, err := ssh.Dial("tcp", addr, cfg)
		ch <- dialRes{cl, err}
	}()
	select {
	case <-ctx.Done():
		return nil, Result{Status: StatusTimeout, Reason: "dial timeout", Fingerprint: holder.get()}, false
	case dr := <-ch:
		if dr.err != nil {
			return nil, classifyDialErr(dr.err, holder), false
		}
		return dr.cl, Result{}, true
	}
}

func (c *Client) runOne(ctx context.Context, client *ssh.Client, command string, holder *fpHolder) Result {
	session, err := client.NewSession()
	if err != nil {
		return Result{Status: StatusError, Reason: err.Error(), Fingerprint: holder.get()}
	}
	defer func() { _ = session.Close() }()

	type runRes struct {
		out []byte
		err error
	}
	ch := make(chan runRes, 1)
	go func() {
		out, err := session.CombinedOutput(command)
		ch <- runRes{out, err}
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return Result{Status: StatusTimeout, Reason: "command timeout", Fingerprint: holder.get()}
	case r := <-ch:
		out, trunc := capOutput(r.out, c.capBytes)
		res := Result{Output: out, Truncated: trunc, Fingerprint: holder.get()}
		switch {
		case r.err == nil:
			res.Status = StatusOK
		case isExitError(r.err):
			// The command ran but exited non-zero — the output is still valid.
			res.Status = StatusOK
		default:
			res.Status = StatusError
			res.Reason = r.err.Error()
		}
		return res
	}
}

func isExitError(err error) bool {
	_, ok := errors.AsType[*ssh.ExitError](err)
	return ok
}

func (c *Client) hostKeyCallback(hostKey string, holder *fpHolder) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		fp := ssh.FingerprintSHA256(key)
		holder.setFP(fp)
		if err := c.known.verify(hostKey, fp); err != nil {
			holder.setMismatch(err.Error())
			return err
		}
		return nil
	}
}

func classifyDialErr(err error, holder *fpHolder) Result {
	if reason, mismatch := holder.mismatchReason(); mismatch {
		return Result{Status: StatusError, Reason: reason, Fingerprint: holder.get()}
	}
	msg := err.Error()
	if strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "deadline exceeded") {
		return Result{Status: StatusTimeout, Reason: msg, Fingerprint: holder.get()}
	}
	return Result{Status: StatusUnreachable, Reason: msg, Fingerprint: holder.get()}
}

func capOutput(b []byte, limit int) (string, bool) {
	if len(b) <= limit {
		return string(b), false
	}
	return string(b[:limit]) + fmt.Sprintf("\n…[truncated %d bytes]", len(b)-limit), true
}

// fpHolder carries the fingerprint (and any mismatch) out of the host-key callback.
type fpHolder struct {
	mu       sync.Mutex
	fp       string
	mismatch bool
	reason   string
}

func (h *fpHolder) setFP(fp string) {
	h.mu.Lock()
	h.fp = fp
	h.mu.Unlock()
}

func (h *fpHolder) setMismatch(reason string) {
	h.mu.Lock()
	h.mismatch = true
	h.reason = reason
	h.mu.Unlock()
}

func (h *fpHolder) get() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fp
}

func (h *fpHolder) mismatchReason() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reason, h.mismatch
}

// fingerprintStore is the TOFU fingerprint store (in-memory, per-process).
type fingerprintStore struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *fingerprintStore) verify(host, fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.m[host]
	if !ok {
		s.m[host] = fp
		return nil
	}
	if prev != fp {
		return fmt.Errorf("host key changed for %s: known %s got %s", host, prev, fp)
	}
	return nil
}
