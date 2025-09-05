FROM golang:1.25-bookworm AS builder

# Set build arguments first
ARG COREDNS_VERSION=v1.12.3
ARG PLUGIN_VERSION=dev

# Set the GOPATH and create directories
WORKDIR /go/src

# Clone CoreDNS and build once to cache dependencies (this layer will be cached)
RUN git clone https://github.com/coredns/coredns.git /coredns && \
    cd /coredns && \
    git checkout $COREDNS_VERSION && \
    go mod download && \
    go generate && \
    CGO_ENABLED=0 go build -o /coredns/coredns .

# Copy the local dnsnameresolver plugin (this layer changes when code changes)
COPY . /go/src/dnsnameresolver/

# Build CoreDNS with the dnsnameresolver plugin (reusing cached dependencies)
RUN cd /coredns && \
    sed -i '/file:file/i dnsnameresolver:github.com/kubeovn/dnsnameresolver' plugin.cfg && \
    go mod edit -replace github.com/kubeovn/dnsnameresolver=/go/src/dnsnameresolver && \
    go get github.com/kubeovn/dnsnameresolver && \
    go generate && \
    CGO_ENABLED=0 go build -ldflags "-X github.com/kubeovn/dnsnameresolver.Version=$PLUGIN_VERSION" -o /coredns/coredns .

# Create the final image with CoreDNS binary
FROM debian:bookworm

COPY --from=builder /coredns/coredns /usr/bin/coredns

RUN apt-get update && apt-get upgrade -y
WORKDIR /

ENTRYPOINT ["/usr/bin/coredns"]