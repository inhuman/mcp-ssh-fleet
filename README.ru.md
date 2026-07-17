# mcp-ssh-fleet

[English](README.md) | **Русский**

MCP-сервер: даёт агенту два инструмента поверх SSH к флоту виртуалок вне Kubernetes.
Ключ доступа — секрет пода (вне контекста модели); инвентарь — аллоулист (fail-closed).

## Инструменты

- **`ssh_probe(tags, check)`** — курируемая read-only диагностика на всех хостах,
  несущих указанные теги (AND-семантика, как теги GitLab-раннеров). `check` — из
  зашитого набора: `uptime`, `disk`, `mem`, `failed`, `logs`. Произвольную команду
  передать нельзя. Класс `read-only`.
- **`ssh_exec(host, command)`** — одна произвольная неинтерактивная команда на ОДНОМ
  инвентарном хосте (по имени или адресу; вне инвентаря — отказ). Класс `write-external`:
  сервер выполняет команду, а гейтинг доступа (approval/RBAC) — на стороне MCP-клиента.

Оба: кап на размер вывода, per-host таймаут, TOFU-проверка ключа хоста (отпечаток в
логах), вывод по секции на хост.

## Конфигурация (env)

| Переменная | Дефолт | Смысл |
|---|---|---|
| `SSH_FLEET_TRANSPORT` | `http` | `http` (StreamableHTTP, endpoint `/mcp`) \| `sse` \| `stdio` |
| `SSH_FLEET_ADDR` | `:8080` | адрес для http/sse |
| `SSH_FLEET_AUTH_TOKEN` | — | опц. токен `X-MCP-AUTH` |
| `SSH_FLEET_INVENTORY_PATH` | `/etc/ssh-fleet/inventory.yaml` | путь к инвентарю (configmap) |
| `SSH_FLEET_KEY_PATH` | `/etc/ssh-fleet/id_ed25519` | путь к приватному ключу (секрет) |
| `SSH_FLEET_OUTPUT_CAP_BYTES` | `8192` | кап вывода на секцию |
| `SSH_FLEET_CMD_TIMEOUT_SECONDS` | `20` | per-host таймаут |
| `SSH_FLEET_PROBE_CONCURRENCY` | `8` | пул параллелизма probe |
| `SSH_FLEET_PROBE_MAX_HOSTS` | `50` | предохранитель на число хостов в пробе |

Инвентарь — см. `deploy/inventory.example.yaml`.

## Быстрый старт (docker, stdio)

```sh
docker run -i --rm \
  -v /path/to/inventory.yaml:/etc/ssh-fleet/inventory.yaml:ro \
  -v /path/to/id_ed25519:/etc/ssh-fleet/id_ed25519:ro \
  -e SSH_FLEET_TRANSPORT=stdio \
  ghcr.io/inhuman/mcp-ssh-fleet:latest
```

Именно так MCP-клиенты запускают сервер при установке из
[MCP Registry](https://registry.modelcontextprotocol.io)
(`io.github.inhuman/mcp-ssh-fleet`).

## Разработка

```sh
make test        # юниты + e2e против настоящего in-process SSH-сервера
make vet
make vulncheck
make build
make docker
```

Релиз: тег `vX.Y.Z` → GitHub Actions собирает и публикует multi-arch образ
в `ghcr.io/inhuman/mcp-ssh-fleet`.

## Подключение к MCP-клиенту

По умолчанию сервер говорит на StreamableHTTP; регистрируется как обычный HTTP
MCP-сервер с endpoint `/mcp` (URL вида `http://<host>:8080/mcp`). Оба инструмента
становятся доступны клиенту.

Рекомендации по безопасности на стороне клиента:

- `ssh_probe` — `read-only` (только курируемые проверки), можно давать широко.
- `ssh_exec` — произвольное исполнение (`write-external`). Гейтить его доступ на
  стороне клиента (approval / RBAC / allowlist пользователей) — сервер лишь
  выполняет команду на инвентарном хосте, политику доступа он не решает.
