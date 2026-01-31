# Makefile for building godiam rt_hide_oh test

.PHONY: build test manual clean check

# Build the test Docker image
build:
	@echo "Building rt_hide_oh test image..."
	cd ../.. && docker build -f tests/rt_hide_oh/Dockerfile -t godiam-rt-hide-oh-test .

# Run automated test
test:
	@echo "Running rt_hide_oh test..."
	docker run --rm godiam-rt-hide-oh-test

# Run with manual/interactive mode (streaming logs)
manual:
	@echo "Running in manual mode (Ctrl+C to exit)..."
	docker run --rm -it godiam-rt-hide-oh-test manual

# Run with shell access
shell:
	docker run --rm -it --entrypoint=/bin/bash godiam-rt-hide-oh-test

# Clean up
clean:
	docker rmi godiam-rt-hide-oh-test 2>/dev/null || true

# Check if Docker is accessible
check:
	@docker info > /dev/null 2>&1 && echo "Docker is accessible" || echo "Docker is not accessible"
