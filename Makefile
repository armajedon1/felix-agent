.PHONY: all build build-all build-linux-amd64 build-linux-arm64 build-windows-amd64 run test setup clean

all: build

build:
	mkdir -p bin
	go build -o bin/felix cmd/felix/main.go

build-linux-amd64:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/felix-linux-amd64 cmd/felix/main.go

build-linux-arm64:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/felix-linux-arm64 cmd/felix/main.go

build-windows-amd64:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/felix.exe cmd/felix/main.go

build-all: build build-linux-amd64 build-linux-arm64 build-windows-amd64

run: build
	./bin/felix

setup: build
	./bin/felix setup

test:
	go test -count=1 ./...

clean:
	rm -rf bin/felix*
