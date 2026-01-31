# Dockerfile for godiam DRA test
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

# Generate TLS certificates
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/diameter/cert.pem -keyout /etc/diameter/privkey.pem \
    -subj /CN=dra1.access.realm
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/diameter/cert11.pem -keyout /etc/diameter/privkey11.pem \
    -subj /CN=peer11.access.realm
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/diameter/cert12.pem -keyout /etc/diameter/privkey12.pem \
    -subj /CN=peer12.access.realm
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/diameter/cert2.pem -keyout /etc/diameter/privkey2.pem \
    -subj /CN=dra2.backend.realm
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/diameter/cert22.pem -keyout /etc/diameter/privkey22.pem \
    -subj /CN=peer2.backend.realm
RUN openssl dhparam -out /etc/diameter/dh.pem 1024

# Copy configurations
COPY tests/dra/conf/* /conf/

# Copy scripts
COPY tests/dra/entrypoint.sh /entrypoint.sh
COPY tests/dra/test.sh /test.sh
RUN chmod +x /entrypoint.sh /test.sh

# Create log directory
RUN mkdir -p /tmp

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
