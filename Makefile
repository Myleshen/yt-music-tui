.PHONY: build release run auth test fmt vet clean cross macos

BIN := ytm
BUILD_DIR := build
VERSION ?= 0.1.0

build:
	go build -o $(BUILD_DIR)/$(BIN) ./cmd/ytm

release:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BIN) ./cmd/ytm

run:
	go run ./cmd/ytm

auth:
	go run ./cmd/ytm auth

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

# Cross-compile standalone binaries for common targets.
cross:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o $(BUILD_DIR)/$(BIN)-linux-amd64   ./cmd/ytm
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o $(BUILD_DIR)/$(BIN)-linux-arm64   ./cmd/ytm
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -o $(BUILD_DIR)/$(BIN)-darwin-amd64  ./cmd/ytm
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o $(BUILD_DIR)/$(BIN)-darwin-arm64  ./cmd/ytm
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(BIN)-windows-amd64.exe ./cmd/ytm

deps:
	go mod tidy

# macos builds ytm into a minimal .app bundle. Media keys and the Now Playing
# widget only reach processes the system recognises as applications, which means
# a bundle with a CFBundleIdentifier — see packaging/darwin/Info.plist.
macos:
	rm -rf $(BUILD_DIR)/ytm.app
	mkdir -p $(BUILD_DIR)/ytm.app/Contents/MacOS
	CGO_ENABLED=0 GOOS=darwin go build -trimpath \
		-ldflags "-s -w -X main.version=$(VERSION)" \
		-o $(BUILD_DIR)/ytm.app/Contents/MacOS/$(BIN) ./cmd/ytm
	cp packaging/darwin/Info.plist $(BUILD_DIR)/ytm.app/Contents/Info.plist
	@echo "built $(BUILD_DIR)/ytm.app"
	@echo "run it with: $(BUILD_DIR)/ytm.app/Contents/MacOS/$(BIN)"

clean:
	rm -rf $(BUILD_DIR)
