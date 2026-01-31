# Makefile for godiam DRA performance test

.PHONY: build test clean help

IMAGE := godiam-dra-perf

help:
	@echo "godiam DRA Performance Test"
	@echo ""
	@echo "Targets:"
	@echo "  build  - Build the Docker image"
	@echo "  test   - Run performance test with default parameters"
	@echo "  clean  - Remove Docker image"
	@echo ""
	@echo "Environment variables:"
	@echo "  BENCH_CONNECTIONS - Number of concurrent connections (default: 10)"
	@echo "  BENCH_DURATION    - Test duration (default: 10s)"
	@echo "  BENCH_RATE        - Target requests per second (default: 1000)"
	@echo ""
	@echo "Example:"
	@echo "  make build"
	@echo "  make test"
	@echo "  BENCH_RATE=5000 BENCH_CONNECTIONS=20 make test"

build:
	@echo "Building DRA performance test image..."
	cd ../.. && docker build -f tests/dra_perf/Dockerfile -t $(IMAGE) .

test:
	@echo "Running DRA performance test..."
	docker run --rm \
		-e BENCH_CONNECTIONS=$(or $(BENCH_CONNECTIONS),10) \
		-e BENCH_DURATION=$(or $(BENCH_DURATION),10s) \
		-e BENCH_RATE=$(or $(BENCH_RATE),1000) \
		$(IMAGE)

# Quick test with lower rate
quick:
	@echo "Running quick performance test..."
	docker run --rm \
		-e BENCH_CONNECTIONS=5 \
		-e BENCH_DURATION=5s \
		-e BENCH_RATE=500 \
		$(IMAGE)

# High load test
stress:
	@echo "Running stress test..."
	docker run --rm \
		-e BENCH_CONNECTIONS=50 \
		-e BENCH_DURATION=30s \
		-e BENCH_RATE=10000 \
		$(IMAGE)

clean:
	docker rmi $(IMAGE) 2>/dev/null || true
