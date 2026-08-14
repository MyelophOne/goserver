FROM golang:alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS="-trimpath" go build -pgo=auto -ldflags="-s -w -X github.com/myelophone/goserver.AppEnv=prod -X github.com/myelophone/goserver.AppVersion=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" -o /app/goserver ./cmd/main.go

FROM gcr.io/distroless/static:nonroot

WORKDIR /root/

COPY --from=builder /app/assets ./assets
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/goserver .
COPY --from=builder /app/LICENSE ./LICENSE

LABEL org.opencontainers.image.title="@myelophone/goserver"
LABEL org.opencontainers.image.description="High-performance go server by @myeloph.one"
LABEL org.opencontainers.image.authors="Aliaksandr Ivanou"
LABEL org.opencontainers.image.licenses="PolyForm-Noncommercial-1.0.0"
LABEL org.opencontainers.image.vendor="Aliaksandr Ivanou"
LABEL org.opencontainers.image.source="https://github.com/myelophone/goserver"

EXPOSE 8080

CMD ["./goserver"]
