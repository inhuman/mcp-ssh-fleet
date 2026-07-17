# syntax=docker/dockerfile:1

# Build stage — вендоренное дерево для воспроизводимой сборки.
# Pin по multi-arch digest (обновлять осознанно):
#   docker buildx imagetools inspect golang:1.26-bookworm
FROM golang:1.26-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS build
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/mcp-ssh-fleet ./cmd/mcp-ssh-fleet

# Runtime stage — distroless static, non-root. SSH-клиент — чистый Go (x/crypto/ssh),
# внешний ssh-бинарь не нужен, поэтому shell/пакеты в образе отсутствуют.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
# Ownership proof for the MCP Registry: the label value MUST equal the `name`
# field in server.json. https://registry.modelcontextprotocol.io
LABEL io.modelcontextprotocol.server.name="io.github.inhuman/mcp-ssh-fleet" \
      org.opencontainers.image.source="https://github.com/inhuman/mcp-ssh-fleet"
COPY --from=build /out/mcp-ssh-fleet /usr/local/bin/mcp-ssh-fleet
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/mcp-ssh-fleet"]
