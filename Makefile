.PHONY: all build run test setup clean

all: build

build:
	mkdir -p bin
	go build -o bin/felix cmd/felix/main.go

run: build
	./bin/felix

setup: build
	./bin/felix setup

test:
	go test -count=1 ./...

clean:
	rm -rf bin/felix
