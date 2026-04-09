BINARY     := trangly
BUILD_DIR  := dist
CMD        := ./cmd/trangly
PKG        := github.com/udaypankhaniya/trangly/pkg/version
CSS_INPUT  := internal/ui/static/style.css
CSS_OUTPUT := internal/ui/static/dist/style.min.css

## Version variables injected via ldflags at build time.
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE       := $(shell date -u +%Y-%m-%d 2>/dev/null || echo unknown)
LDFLAGS    := -s -w \
              -X $(PKG).Version=$(VERSION) \
              -X $(PKG).Commit=$(COMMIT) \
              -X $(PKG).BuildDate=$(DATE)

.PHONY: build run setup test lint sqlc clean install uninstall reset release windows deb rpm apk packages css icons

## Fetch Lucide SVG icons and rebuild the embedded SVG sprite (requires Bun).
icons:
	bun run scripts/build-icons.mjs

## Recompile and minify Tailwind CSS (only needed when adding new utility classes).
## Requires Bun: run `bun install` first, then commit dist/style.min.css.
css:
	bunx tailwindcss -i $(CSS_INPUT) -o $(CSS_OUTPUT) --minify

## Build the binary for the current platform.
build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) $(CMD)

## Build release binaries for linux/amd64 and linux/arm64.
release:
	GOOS=linux GOARCH=amd64  go build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64  $(CMD)
	GOOS=linux GOARCH=arm64  go build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64   $(CMD)

## Run in development mode (auto-sets data-dir to ./dev-data).
run:
	go run $(CMD) start --data-dir ./dev-data --port 2880

## Interactive first-run setup: creates admin account in ./dev-data.
setup:
	go run $(CMD) setup --data-dir ./dev-data

## Run all tests.
test:
	go test ./... -race -count=1

## Run tests with coverage report.
cover:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -html=coverage.out

## Lint with golangci-lint (must be installed separately).
lint:
	golangci-lint run ./...

## Regenerate sqlc query code (requires sqlc CLI).
sqlc:
	cd internal/infra/db && sqlc generate

## Tidy and verify Go modules.
tidy:
	go mod tidy
	go mod verify

## Remove build artefacts.
clean:
	rm -rf $(BUILD_DIR) dev-data coverage.out

## Install Trangly as a systemd service (Linux, requires sudo) or print NSSM guide (Windows).
install: build
	sudo $(BUILD_DIR)/$(BINARY) install

## Remove Trangly systemd service (Linux, requires sudo) or print NSSM guide (Windows).
uninstall:
	sudo $(BUILD_DIR)/$(BINARY) uninstall

## Factory-reset: delete DB, master key, logs, and workspaces in dev-data.
reset:
	go run $(CMD) reset --data-dir ./dev-data

## ── Packaging (requires nfpm) ──────────────────────────────────────────────
## Install nfpm once:  go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
##
## Build .deb packages for amd64 + arm64.
deb: release
	mkdir -p $(BUILD_DIR)
	cp $(BUILD_DIR)/$(BINARY)-linux-amd64 $(BUILD_DIR)/trangly-linux && VERSION=$(VERSION) GOARCH=amd64 nfpm package --config nfpm.yaml --packager deb -t $(BUILD_DIR)/
	cp $(BUILD_DIR)/$(BINARY)-linux-arm64 $(BUILD_DIR)/trangly-linux && VERSION=$(VERSION) GOARCH=arm64 nfpm package --config nfpm.yaml --packager deb -t $(BUILD_DIR)/
	rm -f $(BUILD_DIR)/trangly-linux

## Build .rpm packages for amd64 + arm64.
rpm: release
	mkdir -p $(BUILD_DIR)
	cp $(BUILD_DIR)/$(BINARY)-linux-amd64 $(BUILD_DIR)/trangly-linux && VERSION=$(VERSION) GOARCH=amd64 nfpm package --config nfpm.yaml --packager rpm -t $(BUILD_DIR)/
	cp $(BUILD_DIR)/$(BINARY)-linux-arm64 $(BUILD_DIR)/trangly-linux && VERSION=$(VERSION) GOARCH=arm64 nfpm package --config nfpm.yaml --packager rpm -t $(BUILD_DIR)/
	rm -f $(BUILD_DIR)/trangly-linux

## Build .apk packages for amd64 + arm64 (Alpine Linux).
apk: release
	mkdir -p $(BUILD_DIR)
	cp $(BUILD_DIR)/$(BINARY)-linux-amd64 $(BUILD_DIR)/trangly-linux && VERSION=$(VERSION) GOARCH=amd64 nfpm package --config nfpm.yaml --packager apk -t $(BUILD_DIR)/
	cp $(BUILD_DIR)/$(BINARY)-linux-arm64 $(BUILD_DIR)/trangly-linux && VERSION=$(VERSION) GOARCH=arm64 nfpm package --config nfpm.yaml --packager apk -t $(BUILD_DIR)/
	rm -f $(BUILD_DIR)/trangly-linux

## Build all packages (.deb + .rpm + .apk) for both architectures.
packages: deb rpm apk

## Build Windows executables (.exe) + .zip archives for amd64 + arm64 (requires pwsh).
windows:
	pwsh -NonInteractive -File scripts/build-windows.ps1 -Version "$(VERSION)"
