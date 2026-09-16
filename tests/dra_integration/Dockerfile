# Integration test Dockerfile: freeDiameter (left side) + godiam (right side)
# Tests interoperability between freeDiameter and godiam DRA implementations

# haoli1/freediameter is published for linux/amd64 only. Declared as an ARG
# rather than a constant because BuildKit rejects a literal --platform.
ARG RUNTIME_PLATFORM=linux/amd64

# The freeDiameter runtime image below is published for linux/amd64 only, so the
# godiam binaries must be amd64 as well. Without pinning, the builder stage
# resolves to the host architecture and an arm64 binary lands in an amd64 image,
# where it fails to exec with a misleading "No such file or directory". Build on
# the native platform and cross-compile rather than emulating the Go toolchain.
FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS godiam-builder

# Build godiam
WORKDIR /src
COPY go.mod go.sum ./
COPY . .
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -o diameterd ./cmd/diameterd
RUN go build -o diameterc ./cmd/diameterc

# Runtime image with both freeDiameter and godiam.
FROM --platform=$RUNTIME_PLATFORM haoli1/freediameter:latest

# Install runtime dependencies for godiam
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy godiam binaries from builder
COPY --from=godiam-builder /src/diameterd /usr/local/bin/godiam-diameterd
COPY --from=godiam-builder /src/diameterc /usr/local/bin/godiam-diameterc

# Create directories
RUN mkdir -p /conf /etc/diameter

# Generate TLS certificates for all nodes
# Left side (freeDiameter): dra1, peer11, peer12
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/freeDiameter/cert.pem -keyout /etc/freeDiameter/privkey.pem \
    -subj /CN=dra1.access.realm
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/freeDiameter/cert11.pem -keyout /etc/freeDiameter/privkey11.pem \
    -subj /CN=peer11.access.realm
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/freeDiameter/cert12.pem -keyout /etc/freeDiameter/privkey12.pem \
    -subj /CN=peer12.access.realm

# Right side (godiam): dra2, peer2
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/diameter/cert2.pem -keyout /etc/diameter/privkey2.pem \
    -subj /CN=dra2.backend.realm
RUN openssl req -new -batch -x509 -days 3650 -nodes \
    -newkey rsa:1024 -out /etc/diameter/cert22.pem -keyout /etc/diameter/privkey22.pem \
    -subj /CN=peer2.backend.realm

RUN openssl dhparam -out /etc/freeDiameter/dh.pem 1024

# Copy all configuration files
COPY tests/dra_integration/conf/* /conf/

# Copy test scripts
COPY tests/dra_integration/entrypoint.sh /entrypoint.sh
COPY tests/dra_integration/test.sh /test.sh
RUN chmod +x /entrypoint.sh /test.sh

# Create log directory
RUN mkdir -p /tmp

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
