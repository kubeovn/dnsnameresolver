# Variables
IMAGE_NAME := dnsnameresolver
IMAGE_TAG := dev
COREDNS_VERSION := v1.12.3
PLUGIN_VERSION := dev
REGISTRY ?= 

# Default target
.PHONY: all
all: build

# Build Docker image
.PHONY: build
build:
	docker build \
		--build-arg COREDNS_VERSION=$(COREDNS_VERSION) \
		--build-arg PLUGIN_VERSION=$(PLUGIN_VERSION) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) .

# Build with specific version
.PHONY: build-version
build-version:
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: VERSION is required. Usage: make build-version VERSION=v1.0.0"; \
		exit 1; \
	fi
	docker build \
		--build-arg COREDNS_VERSION=$(COREDNS_VERSION) \
		--build-arg PLUGIN_VERSION=$(VERSION) \
		-t $(IMAGE_NAME):$(VERSION) .

# Test the built image
.PHONY: test
test:
	@echo "Testing CoreDNS version..."
	docker run --rm $(IMAGE_NAME):$(IMAGE_TAG) -version
	@echo "\nTesting plugin integration..."
	docker run --rm $(IMAGE_NAME):$(IMAGE_TAG) -plugins | grep dnsnameresolver

# Run the image (for testing)
.PHONY: run
run:
	docker run --rm -p 53:53/udp $(IMAGE_NAME):$(IMAGE_TAG)

# Push to registry
.PHONY: push
push:
	@if [ -z "$(REGISTRY)" ]; then \
		echo "Error: REGISTRY is required. Usage: make push REGISTRY=your-registry.com"; \
		exit 1; \
	fi
	docker tag $(IMAGE_NAME):$(IMAGE_TAG) $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

# Clean up images
.PHONY: clean
clean:
	docker rmi $(IMAGE_NAME):$(IMAGE_TAG) || true
	docker system prune -f

# Show help
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build         - Build Docker image with default settings"
	@echo "  build-version - Build with specific version (requires VERSION=x.x.x)"
	@echo "  test          - Test the built image"
	@echo "  run           - Run the image for testing (port 53:53/udp)"
	@echo "  push          - Push to registry (requires REGISTRY=xxx)"
	@echo "  clean         - Clean up Docker images"
	@echo "  help          - Show this help message"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make build-version VERSION=v1.0.0"
	@echo "  make push REGISTRY=docker.io/username"
	@echo "  make test"
