# hedgiemate-notifier build/release.
#
# VERSION is baked into the binary (main.version) and reported to the relay so
# it can flag outdated notifiers. No secrets live here — `docker login` (Docker
# Hub + ghcr) and `gh auth` are configured separately.

PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7,linux/arm/v6

.PHONY: release images build test

## release: full release — build+push images, git tag, push tag, GitHub release.
## Usage: make release VERSION=1.4.1
release:
	@test -n "$(VERSION)" || (echo "VERSION required: make release VERSION=1.4.1"; exit 1)
	@git diff --quiet || (echo "working tree dirty — commit first"; exit 1)
	$(MAKE) images VERSION=$(VERSION)
	git tag v$(VERSION)
	git push origin v$(VERSION)
	gh release create v$(VERSION) --title "v$(VERSION)" --generate-notes
	@echo "released v$(VERSION)"

## images: multi-arch build + push to Docker Hub and ghcr, version stamped.
## VERSION defaults to the current git tag/SHA; override with VERSION=x.y.z.
images: VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
images:
	@test -n "$(VERSION)" || (echo "VERSION is empty (tag the repo or pass VERSION=x.y.z)"; exit 1)
	docker buildx build --platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		-t hedgiemate/notifier:latest \
		-t hedgiemate/notifier:$(VERSION) \
		-t ghcr.io/lukstankovic/hedgiemate-notifier:latest \
		-t ghcr.io/lukstankovic/hedgiemate-notifier:$(VERSION) \
		--push .
	@echo "pushed notifier images $(VERSION)"

## build: local single-arch build (no push), version stamped
build: VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
build:
	docker build --build-arg VERSION=$(VERSION) -t hedgiemate/notifier:dev .

## test: run the Go tests
test:
	go test ./...
