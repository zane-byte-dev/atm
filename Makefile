APP     := atm
VERSION ?= 0.6.0
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
