# Makefile for godiam rt_redirect test

.PHONY: build test manual clean check

build:
	@echo "Building rt_redirect test image..."
	cd ../.. && docker build -f tests/rt_redirect/Dockerfile -t godiam-rt-redirect-test .

test:
	@echo "Running rt_redirect test..."
	docker run --rm godiam-rt-redirect-test

manual:
	@echo "Running in manual mode (Ctrl+C to exit)..."
	docker run --rm -it godiam-rt-redirect-test manual

shell:
	docker run --rm -it --entrypoint=/bin/bash godiam-rt-redirect-test

clean:
	docker rmi godiam-rt-redirect-test 2>/dev/null || true

check:
	@docker info > /dev/null 2>&1 && echo "Docker is accessible" || echo "Docker is not accessible"
