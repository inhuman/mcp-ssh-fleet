# mcp-ssh-fleet

**English** | [Русский](README.ru.md)

[![Version](https://img.shields.io/github/v/tag/inhuman/mcp-ssh-fleet?sort=semver&style=flat-square&label=version)](https://github.com/inhuman/mcp-ssh-fleet/tags)
[![MCP Registry](https://img.shields.io/badge/MCP_Registry-io.github.inhuman%2Fmcp--ssh--fleet-blue?style=flat-square)](https://registry.modelcontextprotocol.io)
[![Container](https://img.shields.io/badge/ghcr.io-inhuman%2Fmcp--ssh--fleet-2496ED?style=flat-square&logo=github)](https://github.com/inhuman/mcp-ssh-fleet/pkgs/container/mcp-ssh-fleet)
[![Build](https://img.shields.io/github/actions/workflow/status/inhuman/mcp-ssh-fleet/docker-publish.yml?style=flat-square&logo=github)](https://github.com/inhuman/mcp-ssh-fleet/actions/workflows/docker-publish.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/inhuman/mcp-ssh-fleet?style=flat-square&logo=go)](https://go.dev/)
[![Go Report Card](https://goreportcard.com/badge/github.com/inhuman/mcp-ssh-fleet?style=flat-square)](https://goreportcard.com/report/github.com/inhuman/mcp-ssh-fleet)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow?style=flat-square)](LICENSE)
[![Issues](https://img.shields.io/github/issues/inhuman/mcp-ssh-fleet?style=flat-square)](https://github.com/inhuman/mcp-ssh-fleet/issues)
[![Last Commit](https://img.shields.io/github/last-commit/inhuman/mcp-ssh-fleet?style=flat-square)](https://github.com/inhuman/mcp-ssh-fleet/commits/main)

MCP server that gives an agent two tools over SSH to a fleet of hosts outside
Kubernetes. The SSH key is a mounted secret (never enters model context); the
inventory is a fail-closed allowlist.

## Tools

- **`ssh_probe(tags, check)`** — curated read-only diagnostics on every host
  carrying the given tags (AND semantics, like GitLab runner tags). `check` is
  one of a built-in set: `uptime`, `disk`, `mem`, `failed`, `logs`. Arbitrary
  commands cannot be passed. Class: `read-only`.
- **`ssh_exec(host, command)`** — one arbitrary non-interactive command on ONE
  inventory host (by name or address; hosts outside the inventory are refused).
  Class: `write-external`: the server executes the command, while access gating
  (approval/RBAC) belongs to the MCP client.

Both: output size cap, per-host timeout, TOFU host-key check (fingerprint in
logs), output as one section per host.

## Configuration (env)

| Variable | Default | Meaning |
|---|---|---|
| `SSH_FLEET_TRANSPORT` | `http` | `http` (StreamableHTTP, endpoint `/mcp`) \| `sse` \| `stdio` |
| `SSH_FLEET_ADDR` | `:8080` | listen address for http/sse |
| `SSH_FLEET_AUTH_TOKEN` | — | optional `X-MCP-AUTH` token |
| `SSH_FLEET_INVENTORY_PATH` | `/etc/ssh-fleet/inventory.yaml` | path to the inventory (configmap) |
| `SSH_FLEET_KEY_PATH` | `/etc/ssh-fleet/id_ed25519` | path to the private key (secret) |
| `SSH_FLEET_OUTPUT_CAP_BYTES` | `8192` | output cap per section |
| `SSH_FLEET_CMD_TIMEOUT_SECONDS` | `20` | per-host timeout |
| `SSH_FLEET_PROBE_CONCURRENCY` | `8` | probe parallelism pool |
| `SSH_FLEET_PROBE_MAX_HOSTS` | `50` | safety cap on hosts per probe |

Inventory format — see `deploy/inventory.example.yaml`.

## Quickstart (docker, stdio)

```sh
docker run -i --rm \
  -v /path/to/inventory.yaml:/etc/ssh-fleet/inventory.yaml:ro \
  -v /path/to/id_ed25519:/etc/ssh-fleet/id_ed25519:ro \
  -e SSH_FLEET_TRANSPORT=stdio \
  ghcr.io/inhuman/mcp-ssh-fleet:latest
```

This is the shape MCP clients use when installing from the
[MCP Registry](https://registry.modelcontextprotocol.io)
(`io.github.inhuman/mcp-ssh-fleet`).

## Connecting an MCP client (http/sse)

By default the server speaks StreamableHTTP; register it as a regular HTTP MCP
server with endpoint `/mcp` (URL like `http://<host>:8080/mcp`). Both tools
become available to the client.

Client-side security recommendations:

- `ssh_probe` is `read-only` (curated checks only) and can be granted broadly.
- `ssh_exec` is arbitrary execution (`write-external`). Gate its access on the
  client side (approval / RBAC / user allowlist) — the server merely executes a
  command on an inventory host; it does not decide access policy.

## Development

```sh
make test        # unit tests + e2e against a real in-process SSH server
make vet
make vulncheck
make build
make docker
```

Release: tag `vX.Y.Z` → GitHub Actions builds and publishes a multi-arch image
to `ghcr.io/inhuman/mcp-ssh-fleet`.
