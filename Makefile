.PHONY: build demo test test-race lint vuln run-hub run-daemon docker

build:
	CGO_ENABLED=0 go build -trimpath -o bin/nexus-hub ./cmd/nexus-hub
	CGO_ENABLED=0 go build -trimpath -o bin/nexusd ./cmd/nexusd

# Synthetic host for screenshots and UI development. Never shipped in releases.
demo:
	CGO_ENABLED=0 go build -trimpath -o bin/nexus-demo ./cmd/nexus-demo

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	gofmt -l .
	go vet ./...
	node --check web/assets/app.js
	node --check web/assets/i18n.js
	node --check web/assets/markdown.js
	node --check web/assets/chat-format.js
	node --check web/assets/diff.js
	node --check web/assets/theme.js

web-test:
	cd web && npm test

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

run-hub:
	go run ./cmd/nexus-hub --addr 127.0.0.1:8080 --data-dir ./.nexus

run-daemon:
	go run ./cmd/nexusd run

docker:
	docker build -t ghcr.io/alexanderwagnerdev/agent-nexus:dev .
