BINARY   := relay
CMD_DIR  := ./cmd/relay
BUILD    := go build
GOFLAGS  := -ldflags="-s -w"
PREFIX   ?= /usr/local/bin

.PHONY: build test test-race test-stress cover lint install clean

build:
	$(BUILD) $(GOFLAGS) -o $(BINARY) $(CMD_DIR)

test:
	go test ./... -count=1

test-race:
	go test ./... -count=1 -race

test-stress:
	go test ./internal/store/ -count=1 -race -run TestStressConcurrentSend -v

cover:
	go test ./... -count=1 -coverprofile=coverage.out
	go tool cover -func=coverage.out
	@rm -f coverage.out

lint:
	go vet ./...

install: build
	install -m 755 $(BINARY) $(PREFIX)/$(BINARY)

clean:
	rm -f $(BINARY) coverage.out
