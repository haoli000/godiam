# Makefile for godiam + freeDiameter integration test

.PHONY: help build run clean test manual

IMAGE_NAME := godiam-freediam-integration
GODIAM_ROOT := ../..

help:
	@echo "godiam + freeDiameter Integration Test"
	@echo ""
	@echo "Available targets:"
	@echo "  build   - Build Docker image with both freeDiameter and godiam"
	@echo "  run     - Run automated integration test"
	@echo "  test    - Alias for run"
	@echo "  manual  - Start services in manual mode (for debugging)"
	@echo "  clean   - Remove Docker image"
	@echo ""
	@echo "Example:"
	@echo "  make build && make test"

build:
	@echo "Building integration test image..."
	cd $(GODIAM_ROOT) && docker build -f tests/dra_integration/Dockerfile -t $(IMAGE_NAME) .

run: test

test:
	@echo "Running integration test..."
	docker run --rm $(IMAGE_NAME)

manual:
	@echo "Starting integration test in manual mode..."
	@echo "Use Ctrl+C to stop, logs will be streamed."
	docker run -it --rm $(IMAGE_NAME) --manual

clean:
	@echo "Removing Docker image..."
	docker rmi $(IMAGE_NAME) || true

# Run test and check exit code
check: build
	@echo "Building and running integration test..."
	@if docker run --rm $(IMAGE_NAME); then \
		echo "✓ Integration test passed!"; \
	else \
		echo "✗ Integration test failed!"; \
		exit 1; \
	fi
