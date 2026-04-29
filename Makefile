.PHONY: build test lint fmt clean

build:
	go build -o agent-gate ./cmd/agent-gate

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f agent-gate
