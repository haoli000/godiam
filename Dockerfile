# Multi-stage Dockerfile for building godiam on a Linux host

# Builder stage: install build dependencies (CGO + libsctp) and build binaries
FROM golang:1.23-bullseye AS builder

# Install system dependencies required for SCTP (and general build tools)
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       gcc \
       pkg-config \
       git \
       lksctp-tools \
       libsctp-dev \
       ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Use Go modules; copy only module files first to leverage caching
COPY go.mod go.sum ./
RUN go mod download

# Copy repository
COPY . .

# Build binaries (enable CGO for SCTP support)
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -o /out/diameterd ./cmd/diameterd
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -o /out/diameterc ./cmd/diameterc
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -o /out/diameterbench ./cmd/diameterbench

# Runtime stage: slim Debian image with CA certs
FROM debian:bullseye-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libsctp1 && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -r diameter && useradd -r -g diameter diameter

# Copy built binaries
COPY --from=builder /out/ /usr/local/bin/

# Default working directory for configs
WORKDIR /etc/diameter
RUN chown diameter:diameter /etc/diameter

# Run as non-root
USER diameter

# Expose Diameter port (TCP)
EXPOSE 3868

# Default entrypoint runs the daemon; override to run other tools.
ENTRYPOINT ["/usr/local/bin/diameterd"]
