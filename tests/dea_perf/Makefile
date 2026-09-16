# Makefile for the godiam DEA performance test

.PHONY: build test quick stress clean help

IMAGE := godiam-dea-perf

help:
	@echo "godiam DEA Performance Test"
	@echo ""
	@echo "Measures the cost of the edge controls by benchmarking the same"
	@echo "backend twice: once through a plain relay, once through an edge"
	@echo "agent running screening, rate limiting, topology hiding and edge"
	@echo "routing."
	@echo ""
	@echo "Targets:"
	@echo "  build  - Build the Docker image"
	@echo "  test   - Run the comparison with default parameters"
	@echo "  quick  - Short, low-rate run"
	@echo "  stress - Long, high-rate run"
	@echo "  clean  - Remove the Docker image"
	@echo ""
	@echo "Environment variables:"
	@echo "  BENCH_CONNECTIONS - Concurrent connections (default: 10)"
	@echo "  BENCH_DURATION    - Measured duration (default: 10s)"
	@echo "  BENCH_RATE        - Target requests per second (default: 1000)"
	@echo "  BENCH_WARMUP      - Warmup duration (default: 3s)"
	@echo ""
	@echo "Example:"
	@echo "  make build && make test"
	@echo "  BENCH_RATE=5000 BENCH_CONNECTIONS=20 make test"

build:
	@echo "Building DEA performance test image..."
	cd ../.. && docker build -f tests/dea_perf/Dockerfile -t $(IMAGE) .

test:
	@echo "Running DEA performance test..."
	docker run --rm \
		-e BENCH_CONNECTIONS=$(or $(BENCH_CONNECTIONS),10) \
		-e BENCH_DURATION=$(or $(BENCH_DURATION),10s) \
		-e BENCH_RATE=$(or $(BENCH_RATE),1000) \
		-e BENCH_WARMUP=$(or $(BENCH_WARMUP),3s) \
		$(IMAGE)

quick:
	@echo "Running quick performance test..."
	docker run --rm \
		-e BENCH_CONNECTIONS=5 \
		-e BENCH_DURATION=5s \
		-e BENCH_RATE=500 \
		-e BENCH_WARMUP=2s \
		$(IMAGE)

stress:
	@echo "Running stress test..."
	docker run --rm \
		-e BENCH_CONNECTIONS=50 \
		-e BENCH_DURATION=30s \
		-e BENCH_RATE=10000 \
		-e BENCH_WARMUP=5s \
		$(IMAGE)

clean:
	docker rmi $(IMAGE) 2>/dev/null || true
