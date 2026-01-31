# Makefile for building godiam rt_session_bind test

.PHONY: build test manual clean check

# Build the test Docker image
build:
	@echo "Building rt_session_bind test image..."
	cd ../.. && docker build -f tests/rt_session_bind/Dockerfile -t godiam-rt-session-bind-test .

# Run automated test
test:
	@echo "Running rt_session_bind test..."
	docker run --rm godiam-rt-session-bind-test

# Run with manual/interactive mode (streaming logs)
manual:
	@echo "Running in manual mode (Ctrl+C to exit)..."
	docker run --rm -it godiam-rt-session-bind-test manual

# Run with shell access
shell:
	docker run --rm -it --entrypoint=/bin/bash godiam-rt-session-bind-test

# Clean up
clean:
	docker rmi godiam-rt-session-bind-test 2>/dev/null || true

# Check if Docker is accessible
check:
	@docker info > /dev/null 2>&1 && echo "Docker is accessible" || echo "Docker is not accessible"
