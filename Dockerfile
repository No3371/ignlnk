# ignlnk serious test image
# Builds the CLI, runs unit tests, and executes the manual test procedure (scriptable subset).
#
# Usage:
#   docker build -t ignlnk-test .
#   docker run --rm ignlnk-test
#
# Or with shell for interactive debugging:
#   docker run --rm -it ignlnk-test /bin/sh

FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy module files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o ignlnk . \
    && go vet ./... \
    && go test -v ./...

# Minimal runtime image
FROM alpine:3.19

RUN apk add --no-cache bash

WORKDIR /app

COPY --from=builder /build/ignlnk /app/ignlnk
COPY tests/docker-test.sh /app/docker-test.sh
RUN sed -i 's/\r$//' /app/docker-test.sh && chmod +x /app/docker-test.sh

ENV IGNLNK=/app/ignlnk
ENV PATH="/app:${PATH}"

# Run full test suite by default
ENTRYPOINT ["/bin/bash", "/app/docker-test.sh"]
CMD []
