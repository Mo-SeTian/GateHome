.PHONY: frontend build test linux release
VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X gatehouse/internal/gateway.Version=$(VERSION)

frontend:
	sh sunpanel/build.sh
	npm --prefix frontend ci
	npm --prefix frontend run build

build: frontend
	mkdir -p dist
	go build -trimpath -ldflags='$(LDFLAGS)' -o dist/gatehouse ./cmd/gatehouse

test: frontend
	python3 -B -m unittest discover -s deploy
	npm --prefix frontend test
	npm --prefix sunpanel run test:gatehome
	npm --prefix sunpanel run type-check
	go test -race ./...
	go test -race sun-panel/api/api_v1/panel sun-panel/api/api_v1/system sun-panel/integration sun-panel/lib/imageasset sun-panel/router
	go vet ./...

linux: frontend
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='$(LDFLAGS)' -o dist/gatehouse-linux-amd64 ./cmd/gatehouse
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='$(LDFLAGS)' -o dist/gatehouse-linux-arm64 ./cmd/gatehouse

release: linux
	python3 scripts/package_release.py
