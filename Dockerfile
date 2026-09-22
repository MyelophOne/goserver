FROM node:26-alpine AS web-deps

FROM golang:alpine AS builder

RUN apk add --no-cache git libstdc++

WORKDIR /app

ARG MYELOPHONE_WEB_ENABLED

COPY go.mod go.sum ./

RUN go mod download

COPY . .
COPY --from=web-deps /usr/local/bin/node /usr/local/bin/node

RUN GIT_COMMIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo unknown) && \
    export GIT_COMMIT_HASH && \
    GOSERVER_VERSION=$(go run ./cmd/version 2>/dev/null || echo dev) && \
    export GOSERVER_VERSION && \
    if [ -z "$MYELOPHONE_WEB_ENABLED" ]; then unset MYELOPHONE_WEB_ENABLED; fi && \
    if [ "$(APP_ENV=prod go run ./cmd/webmode)" = "true" ]; then \
	APP_ENV=prod go run -tags "webcli webbuild" ./cmd generate && \
	APP_ENV=prod go run -tags "webcli webbuild" ./cmd build; \
    else \
	mkdir -p dist && APP_ENV=prod CGO_ENABLED=0 go build -tags myelophone_prod -o dist/goserver ./cmd; \
    fi

FROM alpine:latest

WORKDIR /app

RUN apk add --no-cache ca-certificates wget \
	&& addgroup -S goserver \
	&& adduser -S -G goserver -h /app goserver

COPY --chown=goserver:goserver --from=builder /app/dist/ ./
COPY --chown=goserver:goserver --from=builder /app/LICENSE ./LICENSE

USER goserver

LABEL org.opencontainers.image.title="@myelophone/goserver-template"
LABEL org.opencontainers.image.description="Production-ready Go application template with optional SSR web mode by @myeloph.one"
LABEL org.opencontainers.image.authors="Aliaksandr Ivanou"
LABEL org.opencontainers.image.licenses="PolyForm-Noncommercial-1.0.0"
LABEL org.opencontainers.image.vendor="Aliaksandr Ivanou"
LABEL org.opencontainers.image.source="https://github.com/myelophone/goserver-template"

EXPOSE 8080

CMD ["./goserver"]
