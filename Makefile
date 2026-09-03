# RiffHero — Linux-first guitar practice game.
#
# Everything under internal/ builds and tests with no display and no audio
# device, which is what `make check` covers. Only cmd/riffhero needs the X11/GL
# development headers, so `make build`, `make run` and `make check-app` are the
# targets that require `make deps` first.
#
# `make check-audio` is the exception on purpose: it opens a real device, so it
# lives behind a build tag and is never part of `make check`.

BIN := bin/riffhero
PKG := ./cmd/riffhero
GL  := scripts/with-system-gl.sh

APT_PACKAGES := libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev \
	libxi-dev libxxf86vm-dev libgl1-mesa-dev libasound2-dev

.PHONY: help check check-app check-audio vet test race build run demo smoke clean tidy deps \
        dist version-check

help: ## Show the available targets
	@awk 'BEGIN{FS=":.*##"} /^[a-z][a-z-]*:.*##/{printf "  %-7s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: vet test ## Vet and test the packages that need no display or audio device

check-app: ## Vet and test cmd/ too; needs the packages from `make deps`
	go vet ./...
	go test ./cmd/...

check-audio: ## Run the tests that open a real audio device
	go test -tags hardware -v -count=1 ./internal/audio/

race: ## Run the test suite under the race detector
	go test -race ./internal/...

vet: ## Run go vet over the hardware-free packages
	go vet ./internal/...

test: ## Run the test suite
	go test ./internal/...

build: ## Build the binary into bin/
	go build -o $(BIN) $(PKG)

run: build ## Build and launch the app
	$(GL) ./$(BIN) $(ARGS)

demo: build ## Launch the built-in phrase with a scripted player, no hardware
	$(GL) ./$(BIN) --no-audio

smoke: build ## Run the whole practice loop with no window and no device
	./$(BIN) --dry-run $(ARGS)

# --- Release -----------------------------------------------------------------
# Publishing happens in GitHub Actions when a vX.Y.Z tag is pushed. These two
# exist so you can prove the release will work before creating that tag.

VERSION := $(shell ./$(BIN) --version 2>/dev/null | awk '{print $$2}')

dist: build ## Build and package the release archive the way CI does
	@rm -rf dist && mkdir -p dist
	@cp $(BIN) README.md dist/
	@[ -f LICENSE ] && cp LICENSE dist/ || true
	@tar -C dist -czf "riffhero_v$(VERSION)_linux_amd64.tar.gz" .
	@sha256sum riffhero_v$(VERSION)_linux_amd64.tar.gz
	@echo "not published; that only happens from a pushed tag"

version-check: ## Check a tag against the source version (make version-check TAG=v1.0.0)
	@if [ -z "$(TAG)" ]; then echo "usage: make version-check TAG=v1.0.0"; exit 2; fi
	@RIFFHERO_RELEASE_TAG=$(TAG) go test -run TestVersionMatchesTheReleaseTag ./internal/buildinfo/

tidy: ## Tidy the module
	go mod tidy

clean: ## Remove build output
	rm -rf bin dist riffhero_v*_linux_amd64.tar.gz

deps: ## Print the system packages cmd/riffhero needs
	@echo "sudo apt install $(APT_PACKAGES)"
