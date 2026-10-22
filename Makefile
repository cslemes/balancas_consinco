.PHONY: build test build-windows clean

build:
	go build -o bin/balancas-sync ./cmd/balancas-sync

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/balancas-sync.exe ./cmd/balancas-sync

test:
	go test -v ./...

clean:
	rm -rf bin/
