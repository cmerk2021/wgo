BINARY  := wgo
MODULE  := github.com/cmerk2021/wgo
VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet lint release clean install

build:
	go build -o bin/$(BINARY) ./cmd/wgo

test:
	go test ./...

vet:
	go vet ./...

lint: vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed; ran go vet only"

release: clean
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BINARY)-linux-amd64 ./cmd/wgo
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BINARY)-linux-arm64 ./cmd/wgo

install: build
	install -m 0755 bin/$(BINARY) /usr/local/bin/$(BINARY)

clean:
	rm -rf bin dist
