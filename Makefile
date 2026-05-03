.PHONY: build install clean

BINARY := acp
BUILD_DIR := .

build:
	GOCACHE=/tmp/go-build-cache go build -buildvcs=false -o $(BINARY) ./cmd/acp

install: build
	cp $(BINARY) $$HOME/go/bin/$(BINARY) 2>/dev/null || cp $(BINARY) $$HOME/.local/bin/$(BINARY) 2>/dev/null || echo "Install manually: cp $(BINARY) /usr/local/bin/"

clean:
	rm -f $(BINARY)
