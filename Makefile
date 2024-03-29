BUILD_DIR=build
MAIN_FILE=main.go

build-linux:
	mkdir -p build
	GOARCH=amd64 GOOS=linux go build -o ${BUILD_DIR}/lukla-linux-64 ${MAIN_FILE}

build-darwin:
	mkdir -p build
	GOARCH=amd64 GOOS=darwin go build -o ${BUILD_DIR}/lukla-darwin-64 ${MAIN_FILE}

build-windows:
	mkdir -p build
	GOARCH=amd64 GOOS=windows go build -o ${BUILD_DIR}/lukla-windows-64 ${MAIN_FILE}

build:
	make build-linux
	make build-darwin
	make build-windows

run:
	go run ${MAIN_FILE}

test:
	go test ./...

coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

lint:
	golangci-lint run --enable-all

clean:
	go clean
	rm ${BUILD_DIR}/lukla-*