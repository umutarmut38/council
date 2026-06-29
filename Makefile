BINARY := council
CMD := ./cmd/council
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/umutarmut38/council/internal/version.Version=$(VERSION) -X github.com/umutarmut38/council/internal/version.Commit=$(COMMIT) -X github.com/umutarmut38/council/internal/version.Date=$(DATE)

.PHONY: test test-vhs vet build release-snapshot clean demo generate cover install-skill

test:
	go test ./...

test-vhs:
	scripts/test-vhs.sh

# Run the suite with coverage and print the total (matches CI's coverage job).
cover:
	go test ./... -covermode=atomic -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -n 1

vet:
	go vet ./...

# Regenerate the generated regions of the docs (command + config reference).
generate:
	go generate ./...

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

# Install the council-config Agent Skill into your AI CLIs. Override the targets
# with TARGETS=..., e.g. `make install-skill TARGETS="--target claude,codex"`.
TARGETS ?= --all
install-skill:
	scripts/install-skill.sh $(TARGETS)

# Record the terminal demo GIF (docs/assets/council-demo.gif). Requires vhs and
# a configured council on PATH; see docs/demo.md.
demo:
	vhs docs/assets/council-demo.tape

release-snapshot:
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_darwin_amd64/$(BINARY) $(CMD)
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_darwin_arm64/$(BINARY) $(CMD)
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_linux_amd64/$(BINARY) $(CMD)
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_linux_arm64/$(BINARY) $(CMD)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_windows_amd64/$(BINARY).exe $(CMD)
	GOOS=windows GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_windows_arm64/$(BINARY).exe $(CMD)

clean:
	rm -rf bin dist
