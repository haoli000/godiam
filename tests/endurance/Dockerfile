# Dockerfile for godiam endurance test
# Includes iptables for network partition simulation
FROM golang:1.23-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o diameterd ./cmd/diameterd
RUN go build -o diameterbench ./cmd/diameterbench

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    iptables \
    procps \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/diameterd /usr/local/bin/
COPY --from=builder /src/diameterbench /usr/local/bin/

RUN mkdir -p /conf /tmp

COPY tests/endurance/conf/* /conf/
COPY tests/endurance/entrypoint.sh /entrypoint.sh
COPY tests/endurance/test.sh /test.sh
RUN chmod +x /entrypoint.sh /test.sh

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
