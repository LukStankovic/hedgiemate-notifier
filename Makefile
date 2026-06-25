-include .release.env

PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7,linux/arm/v6
DOCKERHUB_REPO ?= hedgiemate/notifier

.PHONY: release images build test changelog dockerhub-description

## release: build+push images, tag, GitHub release, sync CHANGELOG.md.
## Usage: make release VERSION=1.4.1 [NOTES="..." | NOTES_FILE=highlights.md]
release:
	@test -n "$(VERSION)" || (echo "VERSION required: make release VERSION=1.4.1"; exit 1)
	@git diff --quiet || (echo "working tree dirty — commit first"; exit 1)
	$(MAKE) images VERSION=$(VERSION)
	git tag v$(VERSION)
	git push origin v$(VERSION)
	if [ -n "$(NOTES_FILE)" ]; then \
		gh release create v$(VERSION) --title "v$(VERSION)" --notes-file "$(NOTES_FILE)" --generate-notes; \
	elif [ -n "$(NOTES)" ]; then \
		gh release create v$(VERSION) --title "v$(VERSION)" --notes "$(NOTES)" --generate-notes; \
	else \
		gh release create v$(VERSION) --title "v$(VERSION)" --generate-notes; \
	fi
	$(MAKE) changelog VERSION=$(VERSION)
	@echo "released v$(VERSION)"

## changelog: append the GitHub release notes for VERSION to CHANGELOG.md and push.
changelog:
	@test -n "$(VERSION)" || (echo "VERSION required"; exit 1)
	tmp=$$(mktemp); \
	gh release view v$(VERSION) --json body,publishedAt -q '"## v$(VERSION) — " + (.publishedAt | split("T")[0]) + "\n\n" + .body + "\n"' > $$tmp; \
	awk -v f="$$tmp" '{print} /new releases inserted below this line/{print ""; while((getline l < f) > 0) print l}' CHANGELOG.md > CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md; \
	rm -f $$tmp
	git add CHANGELOG.md
	git commit -m "docs: changelog for v$(VERSION)"
	git push
	@echo "updated CHANGELOG.md for v$(VERSION)"

## dockerhub-description: push README + recent changelog as the Docker Hub overview.
CHANGELOG_VERSIONS ?= 5
dockerhub-description:
	@test -n "$(DOCKERHUB_USER)" || (echo "DOCKERHUB_USER required"; exit 1)
	@test -n "$(DOCKERHUB_TOKEN)" || (echo "DOCKERHUB_TOKEN required"; exit 1)
	doc=$$(mktemp); \
	cat README.md > $$doc; \
	if [ -f CHANGELOG.md ]; then \
		printf '\n\n---\n\n# Changelog\n\n' >> $$doc; \
		awk '/new releases inserted below this line/{f=1;next} f{if(/^## /){n++}; if(n>$(CHANGELOG_VERSIONS)) exit; print}' CHANGELOG.md >> $$doc; \
	fi; \
	token=$$(curl -fsSL -H "Content-Type: application/json" -X POST -d '{"username":"$(DOCKERHUB_USER)","password":"$(DOCKERHUB_TOKEN)"}' https://hub.docker.com/v2/users/login/ | jq -r .token); \
	test -n "$$token" -a "$$token" != "null" || { echo "Docker Hub login failed (check DOCKERHUB_USER/DOCKERHUB_TOKEN)"; rm -f $$doc; exit 1; }; \
	payload=$$(jq -n --rawfile d $$doc '{full_description: $$d}'); \
	body=$$(mktemp); \
	code=$$(curl -sS -o $$body -w '%{http_code}' -X PATCH -H "Authorization: JWT $$token" -H "Content-Type: application/json" -d "$$payload" https://hub.docker.com/v2/repositories/$(DOCKERHUB_REPO)/); \
	rm -f $$doc; \
	case "$$code" in 2*) echo "updated Docker Hub description for $(DOCKERHUB_REPO)"; rm -f $$body;; \
		*) echo "Docker Hub description update failed: HTTP $$code"; cat $$body; echo; rm -f $$body; exit 1;; \
	esac

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
