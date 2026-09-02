# RiffHero — Linux-first guitar practice game.
#
# Everything under internal/ builds and tests with no display and no audio
# device, which is what `make check` covers. Only cmd/riffhero needs the X11/GL
# development headers, so `make build`, `make run` and `make check-app` are the
# targets that require `make deps` first.

BIN := bin/riffhero
PKG := ./cmd/riffhero
GL  := scripts/with-system-gl.sh

APT_PACKAGES := libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev \
	libxi-dev libxxf86vm-dev libgl1-mesa-dev libasound2-dev

.PHONY: help check check-app vet test build run clean tidy deps

help: ## Show the available targets
	@awk 'BEGIN{FS=":.*##"} /^[a-z][a-z-]*:.*##/{printf "  %-7s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: vet test ## Vet and test the packages that need no display or audio device

check-app: ## Vet cmd/ too; needs the packages from `make deps`
	go vet ./...

vet: ## Run go vet over the hardware-free packages
	go vet ./internal/...

test: ## Run the test suite
	go test ./internal/...

build: ## Build the binary into bin/
	go build -o $(BIN) $(PKG)

run: build ## Build and launch the app
	$(GL) ./$(BIN)

tidy: ## Tidy the module
	go mod tidy

clean: ## Remove build output
	rm -rf bin

deps: ## Print the system packages cmd/riffhero needs
	@echo "sudo apt install $(APT_PACKAGES)"
