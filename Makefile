.PHONY: test lint lint-fix fmt verify

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
