BINARY := build/gitorum

.PHONY: build test clean

build:
	go build -o $(BINARY) .

test:
	go test -race ./...

clean:
	rm -rf build/
