# Makefile to build and test godiam via Docker

# Docker settings
DOCKER ?= docker
DOCKER_CMD := $(DOCKER)

IMAGE ?= godiam:latest
CONTAINER ?= godiam-test

.PHONY: build gen-config run stop logs test fmt vet lint tidy check unit-test

# ── Code quality ──────────────────────────────────────────────────────

# Format Go source code locally
fmt:
	@echo "Formatting Go source code"
	@gofmt -s -w .

# Verify go.mod/go.sum are tidy
tidy:
	@echo "Checking go.mod tidiness"
	go mod tidy
	@git diff --exit-code go.mod go.sum || (echo "ERROR: go.mod/go.sum not tidy — commit the changes" && exit 1)

# Run Go vet (built-in static analysis)
vet:
	@echo "Running go vet"
	go vet ./...

# Run golangci-lint (comprehensive linter)
lint:
	@echo "Running golangci-lint"
	golangci-lint run

# Run unit tests with race detector
unit-test:
	@echo "Running tests with race detector"
	go test -race -count=1 ./...

# Run all code quality checks in sequence
check: fmt tidy vet lint unit-test
	@echo "All checks passed"

# Build the Docker image
build:
	@echo "Building $(IMAGE)"
	$(DOCKER_CMD) build -t $(IMAGE) -f Dockerfile .

# Generate example configuration (runs container and prints --gen-config output)
# This is a light-weight test to verify the image runs and produces expected output
gen-config:
	@echo "Generating example config"
	$(DOCKER_CMD) run --rm $(IMAGE) --gen-config > diameter.yaml
	@echo "Wrote local file: diameter.yaml"

# Run the daemon in background
# NOTE: bind-mount paths are resolved on the Docker host, not locally.
# Provide CONFIG which must be a path on the Docker host (e.g. /home/user/diameter.yaml)
run:
	@if [ -z "$(CONFIG)" ]; then \
		echo "ERROR: set CONFIG=/path/on/host/diameter.yaml to bind-mount a config file on host"; exit 1; \
	fi
	@echo "Starting container $(CONTAINER)"
	$(DOCKER_CMD) run -d --name $(CONTAINER) -p 3868:3868 -v $(CONFIG):/etc/diameter/diameter.yaml $(IMAGE) --config /etc/diameter/diameter.yaml

# Stop and remove the test container
stop:
	@echo "Stopping and removing container $(CONTAINER)"
	-$(DOCKER_CMD) rm -f $(CONTAINER) 2>/dev/null || true

# Follow container logs
logs:
	$(DOCKER_CMD) logs -f $(CONTAINER)

# High level test: build and generate config to verify build succeeded
test: build gen-config
	@echo "Build and gen-config completed. Inspect diameter.yaml or run 'make run CONFIG=/path/on/host/diameter.yaml' to start."

# Generate a configuration with SCTP enabled (writes local file)
.PHONY: gen-config-sctp
gen-config-sctp:
	@echo "Generating example config with SCTP enabled"
	$(DOCKER_CMD) run --rm $(IMAGE) --gen-config | sed 's/enable_sctp: false/enable_sctp: true/' > diameter_sctp.yaml
	@echo "Wrote local file: diameter_sctp.yaml"

# Run a server container with SCTP port exposed. If CONFIG is set
# it is treated as a path on the Docker host; otherwise the local
# generated file `diameter_sctp.yaml` will be mounted and local
# `docker` is used. The server runs detached as $(CONTAINER)-srv.
.PHONY: run-sctp-server
run-sctp-server:
	@if [ -n "$(CONFIG)" ]; then \
		echo "Starting server container (using $(CONFIG))"; \
		$(DOCKER_CMD) run -d --name $(CONTAINER)-srv -p 3868:3868/sctp -v $(CONFIG):/etc/diameter/diameter.yaml $(IMAGE) --config /etc/diameter/diameter.yaml; \
	else \
		if [ ! -f diameter_sctp.yaml ]; then echo "ERROR: diameter_sctp.yaml not found. Run 'make gen-config-sctp' first."; exit 1; fi; \
		echo "Starting local server container and exposing SCTP port"; \
		docker run -d --name $(CONTAINER)-srv -p 3868:3868/sctp -v $(PWD)/diameter_sctp.yaml:/etc/diameter/diameter.yaml $(IMAGE) --config /etc/diameter/diameter.yaml; \
	fi

# Run the diameterc client against the running server using SCTP.
# This starts a short-lived client container which shares the server's
# network namespace so SCTP sockets can be used via localhost.
.PHONY: run-diameterc-sctp
run-diameterc-sctp:
	@echo "Running diameterc client (SCTP) against container $(CONTAINER)-srv"
	@if [ -n "$(CONFIG)" ]; then \
		echo "Running diameterc client using config $(CONFIG)"; \
		$(DOCKER_CMD) run --rm --name $(CONTAINER)-c --network container:$(CONTAINER)-srv -v $(CONFIG):/etc/diameter/diameter.yaml $(IMAGE) /usr/local/bin/diameterc --config /etc/diameter/diameter.yaml --network sctp --host 127.0.0.1 --port 3868 --ccr; \
	else \
		if [ ! -f diameter_sctp.yaml ]; then echo "ERROR: diameter_sctp.yaml not found. Run 'make gen-config-sctp' first."; exit 1; fi; \
		echo "Running diameterc client using local config diameter_sctp.yaml"; \
		docker run --rm --name $(CONTAINER)-c --network container:$(CONTAINER)-srv -v $(PWD)/diameter_sctp.yaml:/etc/diameter/diameter.yaml $(IMAGE) /usr/local/bin/diameterc --config /etc/diameter/diameter.yaml --network sctp --host 127.0.0.1 --port 3868 --ccr; \
	fi

# Stop and remove SCTP server and client containers
.PHONY: stop-sctp
stop-sctp:
	@echo "Stopping and removing SCTP containers: $(CONTAINER)-srv $(CONTAINER)-c"
	-$(DOCKER_CMD) rm -f $(CONTAINER)-c $(CONTAINER)-srv 2>/dev/null || true

# Helpful target to show docker client/daemon info
info:
	@echo "Using docker client: $(shell which $(DOCKER) || echo not-found)"
	$(DOCKER_CMD) info || true
