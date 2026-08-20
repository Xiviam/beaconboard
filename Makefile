.PHONY: build check fmt run test

build:
	mkdir -p dist
	go build -trimpath -o dist/beaconboard ./cmd/beaconboard

check:
	go vet ./...
	go test -race -shuffle=on ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

run:
	go run ./cmd/beaconboard -config config.example.json

test:
	go test -shuffle=on ./...
