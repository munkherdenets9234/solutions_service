.PHONY: test build vet

# Full integration suite - spins up a disposable MongoDB per test via
# testcontainers, so this needs Docker running locally. Run this before
# pushing to production.
test:
	go vet ./...
	go test ./... -v -count=1

build:
	go build ./...

vet:
	go vet ./...
