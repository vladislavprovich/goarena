# goarena - common developer tasks. Requires Go >= 1.22 and a C compiler
# (cgo). On Windows use build.ps1 or run "make" from Git Bash / MSYS2.

GO       ?= go
PKGS     := ./...
RESULTS  := results
LOGS     := $(RESULTS)/logs
export CGO_ENABLED := 1

.PHONY: all build vet test race cover fuzz bench micro experiments figures clean

all: vet test build

build:
	$(GO) build $(PKGS)
	$(GO) build -o bin/goarena-bench ./cmd/goarena-bench
	$(GO) build -o bin/httpcache ./examples/httpcache

vet:
	$(GO) vet $(PKGS)

test:
	$(GO) test -count=1 $(PKGS)

race:
	$(GO) test -race -count=1 $(PKGS)

cover:
	$(GO) test -count=1 -coverprofile=$(RESULTS)/coverage.out $(PKGS)
	$(GO) tool cover -func=$(RESULTS)/coverage.out

fuzz:
	$(GO) test -run XXX -fuzz FuzzStore -fuzztime 60s ./store

micro:
	$(GO) test -run XXX -bench . -benchmem -count 5 ./internal/cmem ./arena ./store

experiments:
	$(GO) build -o bin/goarena-bench ./cmd/goarena-bench
	./bin/goarena-bench matrix -exp E1,E1b,E2,E4,E5,E6,E7 -out $(RESULTS)/raw
	./bin/goarena-bench aggregate -in $(RESULTS)/raw -out $(RESULTS)

clean:
	rm -rf bin
