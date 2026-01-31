# Makefile for godiam endurance test

.PHONY: build test quick stress manual shell clean help

IMAGE := godiam-endurance-test

help:
	@echo "godiam Endurance Test"
	@echo ""
	@echo "Targets:"
	@echo "  build   - Build the Docker image"
	@echo "  test    - Run endurance test (default settings)"
	@echo "  quick   - Quick test (shorter phases)"
	@echo "  stress  - Extended stress test"
	@echo "  manual  - Interactive mode with log streaming"
	@echo "  shell   - Shell into the test container"
	@echo "  clean   - Remove Docker image"
	@echo ""
	@echo "Environment variables:"
	@echo "  PHASE_DURATION  - Base phase duration in seconds (default: 20)"
	@echo "  BENCH_RATE      - Requests per second (default: 500)"
	@echo "  BENCH_CONN      - Concurrent connections (default: 10)"
	@echo "  BURST_RATE      - Overload burst rate (default: 3000)"

build:
	@echo "Building endurance test image..."
	cd ../.. && docker build -f tests/endurance/Dockerfile -t $(IMAGE) .

test:
	@echo "Running endurance test..."
	docker run --rm --cap-add NET_ADMIN \
		-e PHASE_DURATION=$(or $(PHASE_DURATION),20) \
		-e BENCH_RATE=$(or $(BENCH_RATE),500) \
		-e BENCH_CONN=$(or $(BENCH_CONN),10) \
		-e BURST_RATE=$(or $(BURST_RATE),3000) \
		$(IMAGE)

quick:
	@echo "Running quick endurance test..."
	docker run --rm --cap-add NET_ADMIN \
		-e PHASE_DURATION=10 \
		-e BENCH_RATE=200 \
		-e BENCH_CONN=5 \
		-e BURST_RATE=1000 \
		$(IMAGE)

stress:
	@echo "Running extended stress test..."
	docker run --rm --cap-add NET_ADMIN \
		-e PHASE_DURATION=60 \
		-e BENCH_RATE=2000 \
		-e BENCH_CONN=20 \
		-e BURST_RATE=5000 \
		$(IMAGE)

manual:
	docker run --rm -it --cap-add NET_ADMIN $(IMAGE) manual

shell:
	docker run --rm -it --cap-add NET_ADMIN $(IMAGE) bash

clean:
	docker rmi $(IMAGE) 2>/dev/null || true
