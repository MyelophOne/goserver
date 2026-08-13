FROM golang:alpine

RUN apk add --no-cache git curl bash build-base

RUN sh -c "$(curl -fsSL https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin

RUN go install github.com/air-verse/air@latest && \
	cp "$(go env GOPATH)/bin/air" /usr/local/bin/air && \
	rm -rf "$(go env GOPATH)"

ENV GOLANGCI_LINT_VERSION=v1.61.0
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
	sh -s -- -b /usr/local/bin ${GOLANGCI_LINT_VERSION}

RUN go install golang.org/x/vuln/cmd/govulncheck@latest && \
	cp "$(go env GOPATH)/bin/govulncheck" /usr/local/bin/govulncheck && \
	rm -rf "$(go env/GOPATH)"

WORKDIR /app
