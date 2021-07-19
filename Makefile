BEAT_NAME = bkunifylogbeat
VERSION = $(shell cat VERSION).$(shell git describe --dirty="-dev" --always --match "NOT A TAG")

PROJECTDIR = $(shell pwd)
SCIRPTDIR = ${PROJECTDIR}/script
OUTPUTDIR = ${PROJECTDIR}/bin
BUILDDIR = ${PROJECTDIR}/build

RELEASE_GOOS = linux windows
RELEASE_GOARCH = amd64 386

.PHONY: .EXPORT_ALL_VARIABLES

.PHONY: build .EXPORT_ALL_VARIABLES
build: init
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=${VERSION}" -o bin/${BEAT_NAME}

.PHONY: init
init:
	@  echo ${BEAT_NAME}-${VERSION} "building..."

