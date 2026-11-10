.PHONY: build test test-race lint vuln run-hub run-daemon docker

build:
	CGO_ENABLED=0 go build -trimpath -o bin/nexus-hub ./cmd/nexus-hub
	CGO_ENABLED=0 go build -trimpath -o bin/nexusd ./cmd/nexusd

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	gofmt -l .
	go vet ./...
	node --check web/assets/app.js

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

run-hub:
	go run ./cmd/nexus-hub --addr 127.0.0.1:8080 --data-dir ./.nexus

run-daemon:
	go run ./cmd/nexusd run

docker:
	docker build -t ghcr.io/alexanderwagnerdev/agent-nexus:dev .
