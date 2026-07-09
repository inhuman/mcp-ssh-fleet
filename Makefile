.PHONY: build test test-short vet vulncheck vendor docker

build:
	go build -mod=vendor -trimpath -o mcp-ssh-fleet ./cmd/mcp-ssh-fleet

test:
	go test ./... -count=1

test-short:
	go test -short ./...

vet:
	go vet ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

vendor:
	go mod tidy && go mod vendor

docker:
	docker build -t idconstruct/mcp-ssh-fleet:dev .
