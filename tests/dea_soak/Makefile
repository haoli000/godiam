# Makefile for the godiam DEA soak test

.PHONY: build test quick long clean help

IMAGE := godiam-dea-soak

help:
	@echo "godiam DEA Soak Test"
	@echo ""
	@echo "Runs a Diameter Edge Agent under sustained load, connection churn"
	@echo "and a burst, sampling CPU, RSS, heap and goroutines throughout, and"
	@echo "judges the result on resource behaviour rather than throughput."
	@echo ""
	@echo "Targets:"
	@echo "  build - Build the Docker image"
	@echo "  test  - Run the soak (default: 120s phases, ~8 min total)"
	@echo "  quick - Short run for validating the harness itself"
	@echo "  long  - Extended run (600s phases, ~25 min total)"
	@echo "  clean - Remove the Docker image"
	@echo ""
	@echo "Environment variables:"
	@echo "  PHASE_DURATION  - Seconds per load phase (default: 120)"
	@echo "  BENCH_RATE      - Steady requests per second (default: 1000)"
	@echo "  BENCH_CONN      - Concurrent connections (default: 10)"
	@echo "  BURST_RATE      - Burst requests per second (default: 5000)"
	@echo "  CHURN_CYCLES    - Connect/disconnect cycles (default: 8)"
	@echo "  SAMPLE_INTERVAL - Seconds between samples (default: 2)"

build:
	@echo "Building DEA soak test image..."
	cd ../.. && docker build -f tests/dea_soak/Dockerfile -t $(IMAGE) .

test:
	docker run --rm \
		-e PHASE_DURATION=$(or $(PHASE_DURATION),120) \
		-e BENCH_RATE=$(or $(BENCH_RATE),1000) \
		-e BENCH_CONN=$(or $(BENCH_CONN),10) \
		-e BURST_RATE=$(or $(BURST_RATE),5000) \
		-e CHURN_CYCLES=$(or $(CHURN_CYCLES),8) \
		$(IMAGE)

quick:
	docker run --rm \
		-e PHASE_DURATION=30 \
		-e BENCH_RATE=500 \
		-e BENCH_CONN=5 \
		-e BURST_RATE=2000 \
		-e CHURN_CYCLES=3 \
		$(IMAGE)

long:
	docker run --rm \
		-e PHASE_DURATION=600 \
		-e BENCH_RATE=2000 \
		-e BENCH_CONN=20 \
		-e BURST_RATE=8000 \
		-e CHURN_CYCLES=20 \
		$(IMAGE)

clean:
	docker rmi $(IMAGE) 2>/dev/null || true
