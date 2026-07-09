// Package sshx — SSH-клиент: одно соединение → одна неинтерактивная команда →
// закрытие. Приватный ключ читается из секрета пода и никогда не сериализуется в
// вывод/лог. Проверка ключа хоста — TOFU (доверие при первом контакте + фиксация
// отпечатка); несовпадение на последующем контакте отклоняет соединение.
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

// New читает приватный ключ из keyPath (секрет пода) и строит клиента. Отсутствие/
// битый ключ — ошибка старта (сервер не должен молча принимать вызовы, которые все
// упадут).
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

// Run подключается к цели и выполняет одну неинтерактивную команду. Операционные
// сбои (недоступность/таймаут/несовпадение ключа) кодируются в Result.Status, а не
// возвращаются ошибкой.
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
			// Команда отработала, но вернула ненулевой код — вывод валиден.
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
	return string(b[:limit]) + fmt.Sprintf("\n…[обрезано %d байт]", len(b)-limit), true
}

// fpHolder переносит отпечаток (и факт несовпадения) из host-key-колбэка наружу.
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

// fingerprintStore — TOFU-хранилище отпечатков (in-memory, per-process в MVP).
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
