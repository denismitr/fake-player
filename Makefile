.PHONY: test
test:
	go test -race -v -count=10 ./...