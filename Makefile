# Makefile for building CoreDNS
GITCOMMIT:=$(shell git describe --dirty --always)
BINARY:=coredns
SYSTEM:=
CHECKS:=check
BUILDOPTS:=-v
GOPATH?=$(HOME)/go
MAKEPWD:=$(dir $(realpath $(firstword $(MAKEFILE_LIST))))
CGO_ENABLED?=0
GOOS?=linux
GOARCH?=amd64
VERSION?=latest
REGISTRY?="ghcr.io/kosmos-io"

.PHONY: all
all: coredns

.PHONY: coredns
coredns: $(CHECKS)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=${GOOS} GOARCH=${GOARCH} go build $(BUILDOPTS) -ldflags="-s -w -X github.com/coredns/coredns/coremain.GitCommit=$(GITCOMMIT)" -o $(BINARY)

.PHONY: images
images: coredns
	set -e;\
	docker buildx build --output=type=docker --platform ${GOOS}/${GOARCH} --tag ${REGISTRY}/coredns:${VERSION} .

.PHONY: push-images
upload-images: images
	@echo "push images to $(REGISTRY)"
	docker push ${REGISTRY}/coredns:${VERSION}

.PHONY: check
check: core/plugin/zplugin.go core/dnsserver/zdirectives.go

core/plugin/zplugin.go core/dnsserver/zdirectives.go: plugin.cfg
	go generate coredns.go
	go get

.PHONY: gen
gen:
	go generate coredns.go
	go get

.PHONY: pb
pb:
	$(MAKE) -C pb

.PHONY: clean
clean:
	go clean
	rm -f coredns
