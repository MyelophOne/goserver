FROM node:26-alpine AS web-deps

WORKDIR /app

COPY web/system/tailwind/package.json web/system/tailwind/yarn.lock ./web/system/tailwind/
COPY web/system/client/package.json web/system/client/yarn.lock ./web/system/client/

RUN corepack enable \
    && yarn --cwd web/system/tailwind install \
    && yarn --cwd web/system/client install

FROM golang:alpine AS builder

RUN apk add --no-cache git libstdc++

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .
COPY --from=web-deps /usr/local/bin/node /usr/local/bin/node
COPY --from=web-deps /app/web/system/tailwind/node_modules ./web/system/tailwind/node_modules
COPY --from=web-deps /app/web/system/client/node_modules ./web/system/client/node_modules

RUN APP_ENV=prod GIT_COMMIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo unknown) \
    go run -tags webcli ./cmd generate \
    && APP_ENV=prod GIT_COMMIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo unknown) \
    go run -tags webcli ./cmd build

FROM alpine:latest

WORKDIR /app

RUN apk add --no-cache ca-certificates \
    && addgroup -S goserver \
    && adduser -S -G goserver -h /app goserver

COPY --chown=goserver:goserver --from=builder /app/dist/ ./
COPY --chown=goserver:goserver --from=builder /app/LICENSE ./LICENSE

USER goserver

LABEL org.opencontainers.image.title="@myelophone/goserver"
LABEL org.opencontainers.image.description="High-performance Go server with optional SSR web mode by @myeloph.one"
LABEL org.opencontainers.image.authors="Aliaksandr Ivanou"
LABEL org.opencontainers.image.licenses="PolyForm-Noncommercial-1.0.0"
LABEL org.opencontainers.image.vendor="Aliaksandr Ivanou"
LABEL org.opencontainers.image.source="https://github.com/myelophone/goserver"

EXPOSE 8080

CMD ["./goserver"]
