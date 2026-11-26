# Build:                             make
# System-wide or packaged install:   make install [PREFIX=/usr/local] [DESTDIR=]
# Per-user, without make:            ./install.sh
# The shared MCP server (systemd):   make service (per user); make install puts
#                                    the units in $(PREFIX)/lib/systemd/user
#                                    (then: systemctl --user enable --now darshan-mcp.socket)
PREFIX ?= /usr/local
BINDIR  = $(DESTDIR)$(PREFIX)/bin
SHARE   = $(DESTDIR)$(PREFIX)/share
UNITDIR = $(DESTDIR)$(PREFIX)/lib/systemd/user

.PHONY: build install uninstall service check test lint dist

build:
	go build -trimpath -o bin/darshan ./cmd/darshan

install: build
	install -Dm755 bin/darshan $(BINDIR)/darshan
	install -Dm644 README.md $(SHARE)/doc/darshan/README.md
	install -Dm644 CHANGELOG.md $(SHARE)/doc/darshan/CHANGELOG.md
	install -Dm644 LICENSE $(SHARE)/licenses/darshan/LICENSE
	install -Dm644 internal/service/darshan-mcp.socket $(UNITDIR)/darshan-mcp.socket
	install -d $(UNITDIR)
	sed 's|@DARSHAN@|$(PREFIX)/bin/darshan|' internal/service/darshan.service > $(UNITDIR)/darshan.service
	chmod 644 $(UNITDIR)/darshan.service

uninstall:
	rm -f $(BINDIR)/darshan
	rm -rf $(SHARE)/doc/darshan $(SHARE)/licenses/darshan
	rm -f $(UNITDIR)/darshan-mcp.socket $(UNITDIR)/darshan.service

# Per user, after a build or an update: darshan-mcp.socket and
# darshan.service in ~/.config/systemd/user, listening, the daemon restarted.
service: build
	./bin/darshan service install

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	shellcheck -S warning darshan.tmux install.sh scripts/darshan-run

# What CI runs.
check: lint test build
	./bin/darshan --version
	./bin/darshan --help >/dev/null

# Release binaries (CI does this on a tag).
dist:
	mkdir -p dist
	for arch in amd64 arm64; do \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags='-s -w' -o dist/darshan-linux-$$arch ./cmd/darshan; \
	done
