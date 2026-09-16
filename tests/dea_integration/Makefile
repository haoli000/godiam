# Makefile for the godiam Diameter Edge Agent integration test

.PHONY: help build run test manual clean check

IMAGE_NAME := godiam-dea-integration
GODIAM_ROOT := ../..

help:
	@echo "godiam Diameter Edge Agent Integration Test"
	@echo ""
	@echo "Available targets:"
	@echo "  build   - Build the Docker image (edge agent + partners)"
	@echo "  test    - Run the automated integration test"
	@echo "  manual  - Start the nodes and keep the container running"
	@echo "  clean   - Remove the Docker image"
	@echo "  check   - Build, run and report the result"
	@echo ""
	@echo "Example:"
	@echo "  make build && make test"

build:
	@echo "Building the edge agent integration image..."
	cd $(GODIAM_ROOT) && docker build -f tests/dea_integration/Dockerfile -t $(IMAGE_NAME) .

run: test

test:
	@echo "Running the edge agent integration test..."
	docker run --rm $(IMAGE_NAME)

manual:
	@echo "Starting the edge agent test bed in manual mode..."
	@echo "Use Ctrl+C to stop, logs will be streamed."
	docker run -it --rm -p 9001:9001 -p 9101:9101 $(IMAGE_NAME) --manual

clean:
	@echo "Removing the Docker image..."
	docker rmi $(IMAGE_NAME) || true

check: build
	@if docker run --rm $(IMAGE_NAME); then \
		echo "✓ Edge agent integration test passed!"; \
	else \
		echo "✗ Edge agent integration test failed!"; \
		exit 1; \
	fi
