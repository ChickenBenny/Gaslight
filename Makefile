.PHONY: build test vet fmt fmt-check lint ci run

build:
	go build ./...

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Not gofmt-ed:"; echo "$$unformatted"; exit 1; \
	fi

# Mirror of the CI pipeline — run before pushing.
ci: fmt-check vet build test

run:
	go run ./cmd/gaslight
