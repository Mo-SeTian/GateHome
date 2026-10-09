FROM node:22-alpine AS frontend
WORKDIR /src/sunpanel
COPY sunpanel/package.json sunpanel/pnpm-lock.yaml ./
RUN npx --yes pnpm@8.15.9 install --frozen-lockfile
COPY sunpanel ./
RUN npx --yes pnpm@8.15.9 run build-only

FROM golang:1.27.1-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY sunpanel/service/go.mod sunpanel/service/go.sum ./sunpanel/service/
RUN go mod download
COPY sunpanel/service ./sunpanel/service
COPY --from=frontend /src/sunpanel/dist ./sunpanel/service/integration/web
COPY cmd ./cmd
COPY internal ./internal
COPY VERSION ./VERSION
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X gatehouse/internal/gateway.Version=$(cat VERSION)" -o /out/gatehouse ./cmd/gatehouse

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 gatehouse \
    && mkdir /config /log /data /sunpanel && chown gatehouse:gatehouse /config /log /data /sunpanel && chmod 700 /config /log /data /sunpanel
COPY --from=builder /out/gatehouse /usr/local/bin/gatehouse
ENV GATEHOUSE_CONTAINER=1
USER gatehouse
WORKDIR /data
VOLUME ["/config", "/log", "/data", "/sunpanel"]
EXPOSE 16666 18080 18443
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -q -O /dev/null http://127.0.0.1:16666/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/gatehouse", "-supervise", "-managed-root", "/data/app", "-config", "/config", "-log", "/log", "-data", "/data"]
