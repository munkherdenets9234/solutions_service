.PHONY: test test-unit test-integration build vet fmt

# Default: everything that runs on the dev toolchain alone — no Docker, no
# network, no database. Covers the router's auth gates, the error envelope,
# the rate limiter, config validation, the locale resolver and the DTO
# projections. Run this constantly.
test: vet test-unit

test-unit:
	go test ./internal/api/... ./internal/config/... ./internal/entitlement/... ./internal/middleware/... ./internal/dto/... ./internal/i18n/... ./pkg/... -count=1

# The full integration suite spins up a disposable MongoDB per test via
# testcontainers, so it needs Docker running locally. Kept as a separate
# target because it cannot run on the dev toolchain alone — and because a
# suite you can only run sometimes should not be the one you type by reflex.
# Run this before pushing to production.
test-integration:
	go test ./test/... ./internal/testutil/... -count=1

build:
	go build ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal pkg
