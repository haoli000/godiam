# Dockerfile for the godiam DEA soak test
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
    python3 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/diameterd /usr/local/bin/
COPY --from=builder /src/diameterbench /usr/local/bin/

RUN mkdir -p /conf /tmp

COPY tests/dea_soak/conf/* /conf/

COPY tests/dea_soak/entrypoint.sh /entrypoint.sh
COPY tests/dea_soak/monitor.py /monitor.py
COPY tests/dea_soak/analyze.py /analyze.py
RUN chmod +x /entrypoint.sh /monitor.py /analyze.py

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
