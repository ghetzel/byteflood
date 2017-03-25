.PHONY: test deps

all: fmt deps build bundle

deps:
	@go list github.com/mjibson/esc || go get github.com/mjibson/esc/...
	go generate -x
	go get .

clean-bundle:
	@test -d public && rm -rf public || true

clean:
	rm -rf vendor bin

fmt:
	gofmt -w .
	go vet .

regen-test-files:
	@-rm -rf "tests/files"
	@mkdir -p "tests/files/music/ABBA/Arrival"
	@dd if=/dev/urandom bs=1 count=180 of="tests/files/music/ABBA/Arrival/01 When I Kissed the Teacher.mp3" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=230 of="tests/files/music/ABBA/Arrival/02 Dancing Queen.mp3" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=170 of="tests/files/music/ABBA/Arrival/03 Dum Dum Diddle.mp3" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=232 of="tests/files/music/ABBA/Arrival/04 My Love, My Life.mp3" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=175 of="tests/files/music/ABBA/Arrival/05 Tiger.mp3" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=185 of="tests/files/music/ABBA/Arrival/06 Money, Money, Money.mp3" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=195 of="tests/files/music/ABBA/Arrival/07 That's Me.mp3" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=200 of="tests/files/music/ABBA/Arrival/08 Why Did It Have to Be Me.mp3" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=242 of="tests/files/music/ABBA/Arrival/09 Knowing Me, Knowing You.mp3" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=180 of="tests/files/music/ABBA/Arrival/10 Arrival.mp3" 2> /dev/null
	@mkdir -p "tests/files/music/Various Artists/A Night at the Roxbury"
	@dd if=/dev/urandom bs=1 count=101 of="tests/files/music/Various Artists/A Night at the Roxbury/01 What Is Love (7' Mix).flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=202 of="tests/files/music/Various Artists/A Night at the Roxbury/02 Bamboogie (Radio Edit).flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=303 of="tests/files/music/Various Artists/A Night at the Roxbury/03 Make That Money (Roxbury Remix).flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=404 of="tests/files/music/Various Artists/A Night at the Roxbury/04 Disco Inferno.flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=505 of="tests/files/music/Various Artists/A Night at the Roxbury/05 Da Ya Think I'm Sexy (Featuring Rod Stewart).flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=606 of="tests/files/music/Various Artists/A Night at the Roxbury/06 Pop Muzik.flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=707 of="tests/files/music/Various Artists/A Night at the Roxbury/07 Insomia (Monster Mix).flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=808 of="tests/files/music/Various Artists/A Night at the Roxbury/08 Be My Lover (Club Mix).flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=909 of="tests/files/music/Various Artists/A Night at the Roxbury/09 This Is Your Night.flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=1010 of="tests/files/music/Various Artists/A Night at the Roxbury/10 Beautiful Life.flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=1111 of="tests/files/music/Various Artists/A Night at the Roxbury/11 Where Do You Go (Ocean Drive Mix).flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=1212 of="tests/files/music/Various Artists/A Night at the Roxbury/12 A Little Bit Of Ecstacy.flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=1313 of="tests/files/music/Various Artists/A Night at the Roxbury/13 What Is Love (Refreshmento Extro Radio Mix).flac" 2> /dev/null
	@dd if=/dev/urandom bs=1 count=1414 of="tests/files/music/Various Artists/A Night at the Roxbury/14 Careless Whisper.flac" 2> /dev/null

test: regen-test-files
	go test -race ./encryption
	go test -race ./peer
	go test -race ./db
	go test -race .

bench:
	go test -race -v -bench=. ./encryption/

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

