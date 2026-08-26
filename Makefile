.PHONY: fmt test vet build check run

fmt:
	gofmt -w cmd internal

test:
	go test ./...

vet:
	go vet ./...

build:
	mkdir -p dist
	go build -trimpath -o dist/openswiftscale ./cmd/openswiftscale

check:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test ./...
	go build ./cmd/openswiftscale

run:
	./scripts/dev.sh
