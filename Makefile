.PHONY: build install clean

BINARY := acp
BUILD_DIR := .

build:
	GOCACHE=/tmp/go-build-cache go build -buildvcs=false -o $(BINARY) ./cmd/acp

install: build
	@dest=""; \
	for d in $(DESTDIR)$$HOME/go/bin $(DESTDIR)$$HOME/.local/bin /usr/local/bin; do \
		if [ -d "$$d" ] || mkdir -p "$$d" 2>/dev/null; then \
			dest="$$d/$(BINARY)"; break; \
		fi; \
	done; \
	if [ -n "$$dest" ]; then \
		cp $(BINARY) "$$dest" && echo "Installed to $$dest"; \
	else \
		echo "Install manually: cp $(BINARY) /usr/local/bin/"; \
	fi

clean:
	rm -f $(BINARY)
