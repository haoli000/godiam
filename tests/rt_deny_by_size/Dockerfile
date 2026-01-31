# Dockerfile for godiam rt_deny_by_size test
FROM golang:1.23-bookworm AS builder

WORKDIR /src
COPY go.mod .
COPY go.sum .
COPY . .
RUN go build -o diameterd ./cmd/diameterd
RUN go build -o diameterc ./cmd/diameterc

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    openssl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/diameterd /usr/local/bin/
COPY --from=builder /src/diameterc /usr/local/bin/

RUN mkdir -p /conf /etc/diameter /tmp

COPY tests/rt_deny_by_size/conf/* /conf/
COPY tests/rt_deny_by_size/entrypoint.sh /entrypoint.sh
COPY tests/rt_deny_by_size/test.sh /test.sh
RUN chmod +x /entrypoint.sh /test.sh

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
