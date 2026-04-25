.PHONY: build test install clean

BIN := kpot
PKG := ./cmd/kpot

build:
	go build -o $(BIN) $(PKG)

test:
	go test ./...

install:
	go install $(PKG)

clean:
	rm -f $(BIN)
