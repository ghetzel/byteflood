.PHONY: test vendor

all: vendor fmt build bundle

update:
	-rm -rf vendor
	govend -u -v -l

vendor:
	go list github.com/govend/govend
	govend -v --strict

clean-bundle:
	@test -d public && rm -rf public || true

clean:
	rm -rf vendor bin

fmt:
	gofmt -w *.go
	gofmt -w ./peer
	gofmt -w ./encryption
	gofmt -w ./scanner
	gofmt -w ./util
	go vet *.go
	go vet ./peer/..
	go vet ./encryption/..
	go vet ./scanner/..
	go vet ./util/..

test:
	# go test -v .
	go test -v ./peer
	# go test -v ./scanner
	go test -v ./encryption

dockrun:
	docker run -it -v $(PWD):/host ubuntu /host/bin/byteflood -c /host/examples/tuned-video.yml seed /host/tests/peer2/

hashtest:
	./bin/byteflood -c test-config.yml scan -t rehash peer1
	cp -v peer1/*.torrent peer2/

bundle: clean-bundle
	@echo "Bundling static resources under ./public/"
	@test -d public && rm -rf public || true
	@mkdir public
	@cp -R static/* public/
	@mkdir public/res
	@for backend in backends/*; do \
		if [ -d "$${backend}/resources" ]; then \
			mkdir public/res/`basename "$${backend}"`; \
			cp -R $${backend}/resources/* public/res/`basename "$${backend}"`; \
		fi \
	done

build: fmt
	go build -o bin/`basename ${PWD}` cli/*.go

