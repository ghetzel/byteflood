.PHONY: test

all: vendor fmt build bundle

update:
	test -d vendor && rm -rf vendor || exit 0
	glide up

vendor:
	go list github.com/Masterminds/glide
	glide install

clean-bundle:
	@test -d public && rm -rf public || true

clean:
	rm -rf vendor bin

fmt:
	gofmt -w .

test:
	# go test -v .
	go test -v ./scanner

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

