# Makefile for godiam dbg_msg_timings test

.PHONY: build test manual clean check

build:
	@echo "Building dbg_msg_timings test image..."
	cd ../.. && docker build -f tests/dbg_msg_timings/Dockerfile -t godiam-dbg-msg-timings-test .

test:
	@echo "Running dbg_msg_timings test..."
	docker run --rm godiam-dbg-msg-timings-test

manual:
	@echo "Running in manual mode (Ctrl+C to exit)..."
	docker run --rm -it godiam-dbg-msg-timings-test manual

shell:
	docker run --rm -it --entrypoint=/bin/bash godiam-dbg-msg-timings-test

clean:
	docker rmi godiam-dbg-msg-timings-test 2>/dev/null || true

check:
	@docker info > /dev/null 2>&1 && echo "Docker is accessible" || echo "Docker is not accessible"
