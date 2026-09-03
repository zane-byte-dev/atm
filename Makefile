APP      ?= atm
BIN_DIR  ?= bin
DIST_DIR ?= dist
NPM      ?= npm
WEB_DIR  := app/web

# Derive the version from git so a locally built binary reports the commit it
# was actually built from, including a -dirty suffix for uncommitted work. A
# hardcoded default would make `make install` self-report a version unrelated to
# the released tags. Releases go through goreleaser, which injects {{.Version}}
# from the tag it is building and never reads this.
GIT_VERSION := $(shell git describe --tags --dirty --always 2>/dev/null | sed 's/^v//')
VERSION ?= $(if $(GIT_VERSION),$(GIT_VERSION),dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: build build-cli install dist clean web-build web-deps

# Keep dependency installation, frontend compilation, and embedding in one
# dependency chain, including when make runs with -j or builds several targets.
web-deps:
	$(NPM) ci --prefix $(WEB_DIR)

web-build: web-deps
	$(NPM) run build --prefix $(WEB_DIR)
	test -s $(WEB_DIR)/dist/index.html

build: web-build
	mkdir -p "$(BIN_DIR)"
	go build -trimpath -tags webui -ldflags "$(LDFLAGS)" -o "$(BIN_DIR)/$(APP)" ./cmd/atm

# This path deliberately has no frontend dependency and works without Node.js.
build-cli:
	mkdir -p "$(BIN_DIR)"
	go build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN_DIR)/$(APP)" ./cmd/atm

install: build
	@if [ "$$(readlink /usr/local/bin/$(APP))" = "$(abspath $(BIN_DIR))/$(APP)" ]; then \
		echo "/usr/local/bin/$(APP) -> $(abspath $(BIN_DIR))/$(APP) (symlink); build already updated it"; \
	else \
		cp "$(BIN_DIR)/$(APP)" /usr/local/bin/$(APP) && echo "installed to /usr/local/bin/$(APP)"; \
	fi

dist: web-build
	@mkdir -p "$(DIST_DIR)"
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		out="$(DIST_DIR)/$(APP)-$${os}-$${arch}"; \
		echo "==> building $${out}"; \
		GOOS=$${os} GOARCH=$${arch} CGO_ENABLED=0 \
			go build -trimpath -tags webui -ldflags "$(LDFLAGS)" -o "$${out}" ./cmd/atm || exit 1; \
	done

clean:
	rm -rf "$(BIN_DIR)" "$(DIST_DIR)" $(WEB_DIR)/dist/
