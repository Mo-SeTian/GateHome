.PHONY: frontend build test linux release
VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X gatehouse/internal/gateway.Version=$(VERSION)

frontend:
	sh sunpanel/build.sh

build: frontend
	mkdir -p dist
	go build -trimpath -ldflags='$(LDFLAGS)' -o dist/gatehouse ./cmd/gatehouse

test: frontend
	go test -race ./...
	go vet ./...

linux: frontend
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='$(LDFLAGS)' -o dist/gatehouse-linux-amd64 ./cmd/gatehouse
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='$(LDFLAGS)' -o dist/gatehouse-linux-arm64 ./cmd/gatehouse

release: linux
	python3 scripts/package_release.py
