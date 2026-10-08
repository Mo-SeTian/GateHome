FROM golang:1.27.1-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY VERSION ./VERSION
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X gatehouse/internal/gateway.Version=$(cat VERSION)" -o /out/gatehouse ./cmd/gatehouse

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 gatehouse \
    && mkdir /data && chown gatehouse:gatehouse /data && chmod 700 /data
COPY --from=builder /out/gatehouse /usr/local/bin/gatehouse
USER gatehouse
WORKDIR /data
VOLUME ["/data"]
EXPOSE 16666 18080 18443
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -q -O /dev/null http://127.0.0.1:16666/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/gatehouse", "-supervise", "-managed-root", "/data/app", "-data", "/data"]
