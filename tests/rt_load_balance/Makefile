# Makefile for godiam rt_load_balance test

.PHONY: build test manual clean check

build:
	@echo "Building rt_load_balance test image..."
	cd ../.. && docker build -f tests/rt_load_balance/Dockerfile -t godiam-rt-load-balance-test .

test:
	@echo "Running rt_load_balance test..."
	docker run --rm godiam-rt-load-balance-test

manual:
	@echo "Running in manual mode (Ctrl+C to exit)..."
	docker run --rm -it godiam-rt-load-balance-test manual

shell:
	docker run --rm -it --entrypoint=/bin/bash godiam-rt-load-balance-test

clean:
	docker rmi godiam-rt-load-balance-test 2>/dev/null || true

check:
	@docker info > /dev/null 2>&1 && echo "Docker is accessible" || echo "Docker is not accessible"
