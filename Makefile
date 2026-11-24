.PHONY: frontend build test linux release
VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X gatehouse/internal/gateway.Version=$(VERSION)
UNAME_SM := $(shell uname -s)/$(shell uname -m)

frontend:
	sh sunpanel/build.sh
	npm --prefix frontend ci
	npm --prefix frontend run build

build: frontend
	mkdir -p dist
	go build -trimpath -ldflags='$(LDFLAGS)' -o dist/gatehouse ./cmd/gatehouse

test: frontend
	npm --prefix frontend test
	npm --prefix sunpanel run test:gatehome
	npm --prefix sunpanel run test:network
	npm --prefix sunpanel run test:icon
	npm --prefix sunpanel run type-check
ifeq ($(UNAME_SM),Darwin/arm64)
	CGO_ENABLED=0 go test ./...
	cd sunpanel/service && CGO_ENABLED=0 go test ./...
else
	go test -race ./...
	cd sunpanel/service && go test -race ./...
endif
	go vet ./...

linux: frontend
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='$(LDFLAGS)' -o dist/gatehouse-linux-amd64 ./cmd/gatehouse
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='$(LDFLAGS)' -o dist/gatehouse-linux-arm64 ./cmd/gatehouse

release: linux
	python3 scripts/package_release.py
