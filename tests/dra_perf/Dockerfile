# Dockerfile for godiam DRA performance test
FROM golang:1.23-bookworm AS builder

# Build godiam binaries
WORKDIR /src
COPY go.mod .
COPY go.sum .
COPY . .
RUN go build -o diameterd ./cmd/diameterd
RUN go build -o diameterbench ./cmd/diameterbench

# Runtime image
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Copy binaries
COPY --from=builder /src/diameterd /usr/local/bin/
COPY --from=builder /src/diameterbench /usr/local/bin/

# Create configuration directories
RUN mkdir -p /conf /tmp

# Copy configurations
COPY tests/dra_perf/conf/* /conf/

# Copy scripts
COPY tests/dra_perf/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
