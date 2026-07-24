.PHONY: build all install clean run fmt-check test race coverage vet staticcheck vuln secrets check

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION)"

build:
	go build -trimpath $(LDFLAGS) -o bin/intun ./cmd/intun

all:
	GOOS=darwin GOARCH=amd64 go build -trimpath $(LDFLAGS) -o bin/intun-darwin-amd64 ./cmd/intun
	GOOS=darwin GOARCH=arm64 go build -trimpath $(LDFLAGS) -o bin/intun-darwin-arm64 ./cmd/intun
	GOOS=linux GOARCH=amd64 go build -trimpath $(LDFLAGS) -o bin/intun-linux-amd64 ./cmd/intun
	GOOS=linux GOARCH=arm64 go build -trimpath $(LDFLAGS) -o bin/intun-linux-arm64 ./cmd/intun
	GOOS=windows GOARCH=amd64 go build -trimpath $(LDFLAGS) -o bin/intun-windows-amd64.exe ./cmd/intun

install: build
	cp bin/intun /usr/local/bin/

run:
	go run ./cmd/intun

clean:
	rm -rf bin/

test:
	go test -v ./...

race:
	go test -race ./...

coverage:
	go test -coverprofile=coverage.out ./...
	./scripts/check-coverage.sh coverage.out

vet:
	go vet ./...

fmt-check:
	test -z "$$(gofmt -l .)"

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

secrets:
	go run github.com/zricethezav/gitleaks/v8@v8.28.0 detect --no-banner --redact --source .
	go run github.com/zricethezav/gitleaks/v8@v8.28.0 dir --no-banner --redact .

check: fmt-check vet staticcheck race coverage vuln secrets
