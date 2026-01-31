# Dockerfile for godiam dbg_msg_timings test
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

COPY tests/dbg_msg_timings/conf/* /conf/
COPY tests/dbg_msg_timings/entrypoint.sh /entrypoint.sh
COPY tests/dbg_msg_timings/test.sh /test.sh
RUN chmod +x /entrypoint.sh /test.sh

WORKDIR /root

ENTRYPOINT ["/entrypoint.sh"]
