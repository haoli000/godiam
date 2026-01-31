# godiam Helm chart Makefile

export DOCKER_HOST ?= tcp://127.0.0.1:2375

RELEASE    ?= godiam
NAMESPACE  ?= default
CHART_DIR  := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT_DIR   := $(abspath $(CHART_DIR)/../..)
IMAGE      ?= godiam
TAG        ?= latest
KIND_CLUSTER ?= haoli1

# Benchmark parameters
BENCH_CONNECTIONS ?= 10
BENCH_DURATION    ?= 10s
BENCH_RATE        ?= 1000
BENCH_TRANSPORT   ?= tcp

.PHONY: lint template build kind-load install uninstall status test quick stress logs clean

## Validate chart
lint:
	helm lint $(CHART_DIR)

## Render templates without installing
template:
	helm template $(RELEASE) $(CHART_DIR)

## Build Docker image on remote Docker host
build:
	docker build -t $(IMAGE):$(TAG) $(ROOT_DIR)

## Load image into kind cluster
kind-load:
	kind load docker-image $(IMAGE):$(TAG) --name $(KIND_CLUSTER)

## Install DRA + server (no benchmark)
install: build kind-load
	helm upgrade --install $(RELEASE) $(CHART_DIR) \
		--namespace $(NAMESPACE) --create-namespace \
		--set image.repository=$(IMAGE) \
		--set image.tag=$(TAG)

## Uninstall everything
uninstall:
	helm uninstall $(RELEASE) --namespace $(NAMESPACE)

## Show release status
status:
	helm status $(RELEASE) --namespace $(NAMESPACE)
	@echo ""
	@kubectl get pods -n $(NAMESPACE) -l app.kubernetes.io/instance=$(RELEASE)

## Run benchmark (build + install + bench job)
## When BENCH_TRANSPORT=sctp, enable both tcp+sctp on DRA/server listeners,
## and use sctp for the bench outbound connections.
ifeq ($(BENCH_TRANSPORT),sctp)
TRANSPORT_ARGS := --set 'dra.transport={tcp,sctp}' --set 'server.transport={tcp,sctp}' --set bench.transport=sctp
else
TRANSPORT_ARGS := --set bench.transport=tcp
endif

test: build kind-load clean
	helm upgrade --install $(RELEASE) $(CHART_DIR) \
		--namespace $(NAMESPACE) --create-namespace \
		--set image.repository=$(IMAGE) \
		--set image.tag=$(TAG) \
		$(TRANSPORT_ARGS) \
		--set bench.enabled=true \
		--set bench.connections=$(BENCH_CONNECTIONS) \
		--set bench.duration=$(BENCH_DURATION) \
		--set bench.rate=$(BENCH_RATE)
	@echo "Waiting for bench job to complete..."
	kubectl wait --for=condition=complete job/$(RELEASE)-bench \
		--namespace $(NAMESPACE) --timeout=120s || true
	@echo ""
	kubectl logs job/$(RELEASE)-bench --namespace $(NAMESPACE)

## Quick validation (low rate, short duration)
quick:
	$(MAKE) test BENCH_RATE=100 BENCH_DURATION=5s BENCH_CONNECTIONS=2

## Stress test (high rate, many connections)
stress:
	$(MAKE) test BENCH_RATE=5000 BENCH_DURATION=30s BENCH_CONNECTIONS=20

## Show logs for all components
logs:
	@echo "--- DRA ---"
	@kubectl logs -n $(NAMESPACE) -l app.kubernetes.io/instance=$(RELEASE),app.kubernetes.io/component=dra --tail=50 2>/dev/null || echo "(no dra pods)"
	@echo ""
	@echo "--- Server ---"
	@kubectl logs -n $(NAMESPACE) -l app.kubernetes.io/instance=$(RELEASE),app.kubernetes.io/component=server --tail=50 2>/dev/null || echo "(no server pods)"
	@echo ""
	@echo "--- Bench ---"
	@kubectl logs -n $(NAMESPACE) job/$(RELEASE)-bench --tail=50 2>/dev/null || echo "(no bench job)"

## Delete bench job (allows re-running test)
clean:
	kubectl delete job $(RELEASE)-bench --namespace $(NAMESPACE) --ignore-not-found
