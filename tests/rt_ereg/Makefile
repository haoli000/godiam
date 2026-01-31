# Makefile for godiam rt_ereg test

.PHONY: build test manual clean check

build:
	@echo "Building rt_ereg test image..."
	cd ../.. && docker build -f tests/rt_ereg/Dockerfile -t godiam-rt-ereg-test .

test:
	@echo "Running rt_ereg test..."
	docker run --rm godiam-rt-ereg-test

manual:
	@echo "Running in manual mode (Ctrl+C to exit)..."
	docker run --rm -it godiam-rt-ereg-test manual

shell:
	docker run --rm -it --entrypoint=/bin/bash godiam-rt-ereg-test

clean:
	docker rmi godiam-rt-ereg-test 2>/dev/null || true

check:
	@docker info > /dev/null 2>&1 && echo "Docker is accessible" || echo "Docker is not accessible"
