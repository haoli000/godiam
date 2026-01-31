# Makefile for building godiam DRA test

.PHONY: build test manual clean check

# Build the test Docker image
build:
	@echo "Building DRA test image..."
	cd ../.. && docker build -f tests/dra/Dockerfile -t godiam-dra-test .

# Run automated test
test: 
	@echo "Running DRA test..."
	docker run --rm godiam-dra-test

# Run with manual/interactive mode (streaming logs)
manual:
	@echo "Running in manual mode (Ctrl+C to exit)..."
	docker run --rm -it godiam-dra-test manual

# Run with shell access
shell:
	docker run --rm -it --entrypoint=/bin/bash godiam-dra-test

# Clean up
clean:
	docker rmi godiam-dra-test 2>/dev/null || true

# Check if Docker is accessible
check:
	@docker info > /dev/null 2>&1 && echo "Docker is accessible" || echo "Docker is not accessible"
