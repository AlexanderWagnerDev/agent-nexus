.PHONY: build test lint run-hub run-daemon docker

build:
	CGO_ENABLED=0 go build -trimpath -o bin/nexus-hub ./cmd/nexus-hub
	CGO_ENABLED=0 go build -trimpath -o bin/nexusd ./cmd/nexusd

test:
	go test ./...

lint:
	gofmt -l .
	go vet ./...
	node --check web/assets/app.js

run-hub:
	go run ./cmd/nexus-hub --addr 127.0.0.1:8080 --data-dir ./.nexus

run-daemon:
	go run ./cmd/nexusd run

docker:
	docker build -t agentnexus/hub:dev .
