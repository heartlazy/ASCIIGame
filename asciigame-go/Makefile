GO      ?= go
BINDIR  ?= bin
MODULE   = github.com/heartlazyli/asciigame

.PHONY: all server client test test-race vet fmt tidy clean run-server run-client

all: server client

server:
	$(GO) build -o $(BINDIR)/server ./cmd/server

client:
	$(GO) build -o $(BINDIR)/client ./cmd/client

# Unit tests (protocol conformance etc.)
test:
	$(GO) test ./...

# Race-detector build of the whole tree — required per the port plan.
test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

run-server: server
	./$(BINDIR)/server

run-client: client
	./$(BINDIR)/client

clean:
	rm -rf $(BINDIR)
