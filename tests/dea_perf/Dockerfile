# Dockerfile for the godiam DEA performance test
FROM golang:1.23-bookworm AS builder

WORKDIR /src
COPY go.mod .
COPY go.sum .
COPY . .
RUN go build -o diameterd ./cmd/diameterd
RUN go build -o diameterbench ./cmd/diameterbench

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    python3-minimal \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/diameterd /usr/local/bin/
COPY --from=builder /src/diameterbench /usr/local/bin/

RUN mkdir -p /conf /tmp

COPY tests/dea_perf/conf/* /conf/

COPY tests/dea_perf/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
