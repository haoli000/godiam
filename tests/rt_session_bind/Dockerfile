# Dockerfile for godiam rt_session_bind test
FROM golang:1.23-bookworm AS builder

# Build godiam
WORKDIR /src
COPY go.mod .
COPY go.sum .
COPY . .
RUN go build -o diameterd ./cmd/diameterd
RUN go build -o diameterc ./cmd/diameterc

# Runtime image
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    openssl \
    && rm -rf /var/lib/apt/lists/*

# Copy binaries
COPY --from=builder /src/diameterd /usr/local/bin/
COPY --from=builder /src/diameterc /usr/local/bin/

# Create configuration and directories
RUN mkdir -p /conf /etc/diameter

# Copy configurations
COPY tests/rt_session_bind/conf/* /conf/

# Copy scripts
COPY tests/rt_session_bind/entrypoint.sh /entrypoint.sh
COPY tests/rt_session_bind/test.sh /test.sh
RUN chmod +x /entrypoint.sh /test.sh

# Create log directory
RUN mkdir -p /tmp

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
