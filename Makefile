.PHONY: test deps

all: fmt deps build

deps:
	@go list github.com/mjibson/esc || go get github.com/mjibson/esc/...
	go generate -x
	go get .

clean:
	-rm -rf bin

fmt:
	gofmt -w .
	go vet .

test:
	go test -race ./encryption
	go test -race ./peer
	go test ./db
	go test .

bench:
	go test -race -v -bench=. ./encryption/

build: fmt
	go build -o bin/`basename ${PWD}` cli/*.go
