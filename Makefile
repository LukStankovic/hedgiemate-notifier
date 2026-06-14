# hedgiemate-notifier build/release.
#
# VERSION is baked into the binary (main.version) and reported to the relay so
# it can flag outdated notifiers. Defaults to the current git tag/SHA; override
# explicitly with `make release VERSION=1.3.1`.
#
# No secrets live here — `docker login` (Docker Hub + ghcr) is done separately.

VERSION   ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7,linux/arm/v6

.PHONY: release build test

## release: multi-arch build + push to Docker Hub and ghcr, version stamped
release:
	@test -n "$(VERSION)" || (echo "VERSION is empty (tag the repo or pass VERSION=x.y.z)"; exit 1)
	docker buildx build --platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		-t hedgiemate/notifier:latest \
		-t hedgiemate/notifier:$(VERSION) \
		-t ghcr.io/lukstankovic/hedgiemate-notifier:latest \
		-t ghcr.io/lukstankovic/hedgiemate-notifier:$(VERSION) \
		--push .
	@echo "pushed notifier $(VERSION)"

## build: local single-arch build (no push), version stamped
build:
	docker build --build-arg VERSION=$(VERSION) -t hedgiemate/notifier:dev .

## test: run the Go tests
test:
	go test ./...
