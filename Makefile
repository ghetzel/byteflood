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
	go test ./peer
	go test ./db
	go test .

docker:
	docker run --rm -it -v $(PWD)/bin/byteflood:/usr/bin/byteflood:ro ghetzel/byteflood byteflood run -u

bench:
	go test -race -v -bench=. ./encryption/

build: fmt
	go build -o bin/`basename ${PWD}` cli/*.go
