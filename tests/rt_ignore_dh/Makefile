# Makefile for building godiam rt_ignore_dh test

.PHONY: build test manual clean check

# Build the test Docker image
build:
	@echo "Building rt_ignore_dh test image..."
	cd ../.. && docker build -f tests/rt_ignore_dh/Dockerfile -t godiam-rt-ignore-dh-test .

# Run automated test
test:
	@echo "Running rt_ignore_dh test..."
	docker run --rm godiam-rt-ignore-dh-test

# Run with manual/interactive mode (streaming logs)
manual:
	@echo "Running in manual mode (Ctrl+C to exit)..."
	docker run --rm -it godiam-rt-ignore-dh-test manual

# Run with shell access
shell:
	docker run --rm -it --entrypoint=/bin/bash godiam-rt-ignore-dh-test

# Clean up
clean:
	docker rmi godiam-rt-ignore-dh-test 2>/dev/null || true

# Check if Docker is accessible
check:
	@docker info > /dev/null 2>&1 && echo "Docker is accessible" || echo "Docker is not accessible"
