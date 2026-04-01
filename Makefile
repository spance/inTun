.PHONY: build build-all clean run test vet

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o bin/intun ./cmd/intun

build-all:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/intun-darwin-amd64 ./cmd/intun
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/intun-darwin-arm64 ./cmd/intun
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/intun-linux-amd64 ./cmd/intun
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/intun-linux-arm64 ./cmd/intun
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/intun-windows-amd64.exe ./cmd/intun

run:
	go run ./cmd/intun

clean:
	rm -rf bin/

test:
	go test -v ./...

vet:
	go vet ./...