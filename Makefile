APP     := atm

# Derive the version from git so a locally built binary reports the commit it
# was actually built from, including a -dirty suffix for uncommitted work. A
# hardcoded default would make `make install` self-report a version unrelated to
# the released tags. Releases go through goreleaser, which injects {{.Version}}
# from the tag it is building and never reads this.
GIT_VERSION := $(shell git describe --tags --dirty --always 2>/dev/null | sed 's/^v//')
VERSION ?= $(if $(GIT_VERSION),$(GIT_VERSION),dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: build install dist clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/atm

install: build
	@if [ "$$(readlink /usr/local/bin/$(APP))" = "$(CURDIR)/bin/$(APP)" ]; then \
		echo "/usr/local/bin/$(APP) -> bin/$(APP) (symlink); build already updated it"; \
	else \
		cp bin/$(APP) /usr/local/bin/$(APP) && echo "installed to /usr/local/bin/$(APP)"; \
	fi

dist:
	@rm -rf dist && mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		out=dist/$(APP)-$${os}-$${arch}; \
		echo "==> building $${out}"; \
		GOOS=$${os} GOARCH=$${arch} CGO_ENABLED=0 \
			go build -trimpath -ldflags "$(LDFLAGS)" -o $${out} ./cmd/atm || exit 1; \
	done

clean:
	rm -rf bin/ dist/
