.PHONY: build test lint run tidy

build:
	go build -o daybook ./cmd/daybook

test:
	go test -race -count=1 ./...

lint:
	go vet ./...
	golangci-lint run ./...

run: build
	./daybook serve

tidy:
	go mod tidy
