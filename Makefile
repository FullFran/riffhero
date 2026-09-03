# RiffHero — Linux-first guitar practice game.
#
# The domain packages (internal/...) build and test with no display and no
# audio device. Only cmd/riffhero needs the X11/GL development headers; run
# `make deps` to see how to install them.

BIN := bin/riffhero
PKG := ./cmd/riffhero
GL  := scripts/with-system-gl.sh

APT_PACKAGES := libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev \
	libxi-dev libxxf86vm-dev libgl1-mesa-dev libasound2-dev

.PHONY: help check vet test build run clean tidy deps

help: ## Show the available targets
	@awk 'BEGIN{FS=":.*##"} /^[a-z][a-z-]*:.*##/{printf "  %-7s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: vet test ## Vet and test everything

vet: ## Run go vet
	go vet ./...

test: ## Run the test suite
	go test ./...

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
