.PHONY: test lint lint-fix fmt verify verify-ci

test:
	go test ./...

lint:
	golangci-lint run ./...

lint-fix:
	golangci-lint run --fix ./...

fmt:
	golangci-lint fmt ./...

verify:
	$(MAKE) fmt
	$(MAKE) test
	$(MAKE) lint

verify-ci:
	golangci-lint fmt --diff ./...
	go mod tidy -diff
	go test -race ./...
	golangci-lint run ./...
