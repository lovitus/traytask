APP := traytask

.PHONY: fmt test build run clean release-local

fmt:
	gofmt -w *.go

test:
	go test ./...

build:
	go build -o $(APP) .

run:
	go run .

clean:
	rm -rf dist $(APP) $(APP).exe

release-local: clean
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -o dist/$(APP)-linux-amd64 .
	GOOS=windows GOARCH=amd64 go build -o dist/$(APP)-windows-amd64.exe .
	GOOS=darwin GOARCH=amd64 go build -o dist/$(APP)-darwin-amd64 .
