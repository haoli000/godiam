# Makefile for godiam rt_deny_by_size test

.PHONY: build test manual clean check

build:
	@echo "Building rt_deny_by_size test image..."
	cd ../.. && docker build -f tests/rt_deny_by_size/Dockerfile -t godiam-rt-deny-by-size-test .

test:
	@echo "Running rt_deny_by_size test..."
	docker run --rm godiam-rt-deny-by-size-test

manual:
	@echo "Running in manual mode (Ctrl+C to exit)..."
	docker run --rm -it godiam-rt-deny-by-size-test manual

shell:
	docker run --rm -it --entrypoint=/bin/bash godiam-rt-deny-by-size-test

clean:
	docker rmi godiam-rt-deny-by-size-test 2>/dev/null || true

check:
	@docker info > /dev/null 2>&1 && echo "Docker is accessible" || echo "Docker is not accessible"
