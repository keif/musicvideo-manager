.PHONY: build test lint testdata docker-build

VERSION := $(shell cat VERSION 2>/dev/null || echo dev)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o musicvideo-manager ./cmd/musicvideo-manager

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

testdata:
	./scripts/gen_testdata.sh

docker-build:
	docker build -t musicvideo-manager:local .
