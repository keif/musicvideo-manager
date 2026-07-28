.PHONY: build test lint testdata docker-build

build:
	go build -o musicvideo-manager ./cmd/musicvideo-manager

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

testdata:
	./scripts/gen_testdata.sh

docker-build:
	docker build -t musicvideo-manager:local .
