# hedgiemate-notifier build/release.
#
# VERSION is baked into the binary (main.version) and reported to the relay so
# it can flag outdated notifiers. No secrets live here — `docker login` (Docker
# Hub + ghcr) and `gh auth` are configured separately. Docker Hub description
# push reads DOCKERHUB_USER/DOCKERHUB_TOKEN from .release.env (gitignored); see
# .release.env.dist. Env vars override the file.

# Local, gitignored release secrets (DOCKERHUB_USER, DOCKERHUB_TOKEN). Optional.
-include .release.env

PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7,linux/arm/v6
DOCKERHUB_REPO ?= hedgiemate/notifier

.PHONY: release images build test changelog dockerhub-description

## release: full release — build+push images, git tag, push tag, GitHub release,
## sync CHANGELOG.md, sync Docker Hub description.
## Usage: make release VERSION=1.4.1
## Optional human highlights (prepended above the auto-generated commit list):
##   make release VERSION=1.4.8 NOTES="✨ New: Sentry Mode recording notification"
##   make release VERSION=1.4.8 NOTES_FILE=highlights.md   # multi-line
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
	$(MAKE) dockerhub-description
	@echo "released v$(VERSION)"

## changelog: pull the just-published GitHub release notes into CHANGELOG.md and push.
## GitHub's auto-generated notes are the single source of truth.
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

## dockerhub-description: push README.md + recent changelog as the Docker Hub overview.
## Changelog is inlined (not just linked) so it works while the GitHub repo is private.
## CHANGELOG_VERSIONS controls how many recent versions are embedded (default 5).
## Needs DOCKERHUB_USER and DOCKERHUB_TOKEN (a Docker Hub access token) in the env.
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
	test -n "$$token" -a "$$token" != "null" || { echo "Docker Hub login failed"; rm -f $$doc; exit 1; }; \
	payload=$$(jq -n --rawfile d $$doc '{full_description: $$d}'); \
	curl -fsSL -X PATCH -H "Authorization: JWT $$token" -H "Content-Type: application/json" -d "$$payload" https://hub.docker.com/v2/repositories/$(DOCKERHUB_REPO)/ > /dev/null; \
	rm -f $$doc
	@echo "updated Docker Hub description for $(DOCKERHUB_REPO)"

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
