.PHONY: build test lint fmt clean e2e

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

e2e:
	go test -timeout 60s ./internal/e2e/...
