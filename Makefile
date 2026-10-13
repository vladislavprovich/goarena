# goarena - common developer tasks. Requires Go >= 1.27 and a C compiler
# (cgo). On Windows run "make" from Git Bash / MSYS2 with MinGW-w64 GCC.

GO       ?= go
PKGS     := ./...
RESULTS  := results
EXAMPLES := quickstart typed-arena gc-pacing httpcache
export CGO_ENABLED := 1

.PHONY: all build vet fmt test race cover fuzz micro examples experiments clean

all: vet test build

# build compiles every package and writes the harness and the examples to bin/.
build:
	$(GO) build $(PKGS)
	$(GO) build -o bin/goarena-bench ./cmd/goarena-bench
	$(foreach e,$(EXAMPLES),$(GO) build -o bin/$(e) ./examples/$(e) &&) true

vet:
	$(GO) vet $(PKGS)

# fmt fails if a file is not gofmt-formatted (as in CI).
fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "not gofmt-formatted:"; echo "$$out"; exit 1; fi

test:
	$(GO) test -count=1 $(PKGS)

race:
	$(GO) test -race -count=1 $(PKGS)

cover:
	$(GO) test -count=1 -coverprofile=coverage.out $(PKGS)
	$(GO) tool cover -func=coverage.out

fuzz:
	$(GO) test -run XXX -fuzz FuzzStore -fuzztime 60s ./store

micro:
	$(GO) test -run XXX -bench . -benchmem -count 5 ./internal/cmem ./arena ./store

# examples runs the short, self-terminating examples (httpcache is a server).
examples:
	$(GO) run ./examples/quickstart
	$(GO) run ./examples/typed-arena
	$(GO) run ./examples/gc-pacing

experiments:
	$(GO) build -o bin/goarena-bench ./cmd/goarena-bench
	./bin/goarena-bench matrix -exp E1,E1b,E2,E4,E5,E6,E7 -out $(RESULTS)/raw
	./bin/goarena-bench aggregate -in $(RESULTS)/raw -out $(RESULTS)

clean:
	rm -rf bin results coverage.out
