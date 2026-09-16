# Diameter Edge Agent integration test.
#
# Everything runs inside one container: a godiam DEA with an internal and an
# external zone, an internal application node, and three roaming partners
# (well behaved, hostile, flooding).

FROM golang:1.23-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
COPY . .
RUN go build -o diameterd ./cmd/diameterd

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    procps \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/diameterd /usr/local/bin/godiam-diameterd

RUN mkdir -p /conf
COPY tests/dea_integration/conf/* /conf/

COPY tests/dea_integration/entrypoint.sh /entrypoint.sh
COPY tests/dea_integration/test.sh /test.sh
RUN chmod +x /entrypoint.sh /test.sh

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
