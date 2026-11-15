# Builds the web UIs (the admin UI and the consumer web page), then the Go binary (req_001 D-151,
# D-152). A plain `go build ./...` needs no npm: it embeds only web/admin/dist/.gitkeep and
# web/consumer/dist/.gitkeep and serves a one-page stub instead of each UI.
# The web toolchain is Node.js 24 LTS (web/.nvmrc) with npm workspaces under web/.
# `make web-admin-test` and `make web-consumer-test` are the fast unit checks; `make web-e2e` (also
# `make web-admin-e2e`) is the browser end-to-end test.
# `make release-build` builds the six release binaries and dist/SHA256SUMS; `make release-dry` is
# the local run of it (PLT-C-003, req_001 D-30, D-62, D-156).

WEB := web
ADMIN_DIST := $(WEB)/admin/dist
CONSUMER_DIST := $(WEB)/consumer/dist

# The version string compiled into the binary (`local-server --version`): the tag when built from a
# tag, else the nearest tag with the commit, else "dev". The release workflow passes the tag.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build web-deps web-gen web-admin-build web-admin-test web-e2e web-admin-e2e web-consumer-build web-consumer-test clean-web release-build release-dry

# The binary with the built admin UI and consumer web page inside.
build: web-admin-build web-consumer-build
	CGO_ENABLED=0 go build -o bin/local-server ./cmd/local-server

# Installs the locked web dependencies when they are missing.
web-deps:
	@if [ ! -d $(WEB)/node_modules ]; then cd $(WEB) && npm ci; fi

# Generates the TypeScript API types (web/shared/src/api/schema.gen.ts) from api/openapi.yaml.
web-gen: web-deps
	cd $(WEB) && npm run gen

# Builds the admin UI into web/admin/dist, after removing the last build except .gitkeep.
web-admin-build: web-gen
	find $(ADMIN_DIST) -mindepth 1 ! -name .gitkeep -delete
	cd $(WEB) && npm run build:admin

# The web check: a clean install, the generated types, the type check, the unit and component
# tests of shared and admin, the admin build, then the binary with the UI inside.
web-admin-test:
	cd $(WEB) && npm ci
	cd $(WEB) && npm run gen
	cd $(WEB) && npm run typecheck
	cd $(WEB) && npm test
	find $(ADMIN_DIST) -mindepth 1 ! -name .gitkeep -delete
	cd $(WEB) && npm run build:admin
	CGO_ENABLED=0 go build -o bin/local-server ./cmd/local-server

# Builds the consumer web page into web/consumer/dist, after removing the last build except
# .gitkeep.
web-consumer-build: web-gen
	find $(CONSUMER_DIST) -mindepth 1 ! -name .gitkeep -delete
	cd $(WEB) && npm run build:consumer

# The consumer web page check (PLT-F-036): a clean install, the generated types, the type check,
# the unit and component tests of every workspace, the consumer page build, then the binary with
# the page inside.
web-consumer-test:
	cd $(WEB) && npm ci
	cd $(WEB) && npm run gen
	cd $(WEB) && npm run typecheck
	cd $(WEB) && npm test
	find $(CONSUMER_DIST) -mindepth 1 ! -name .gitkeep -delete
	cd $(WEB) && npm run build:consumer
	CGO_ENABLED=0 go build -o bin/local-server ./cmd/local-server

# The browser end-to-end tests (web/e2e, Playwright, Chromium): the built admin UI and consumer web
# page inside the Go harness TestE2EServer (the real serve with the fake engine, an in-memory
# keychain and a fake Tailscale whose listeners are redirected to loopback), compiled as a test
# binary so it gets the stop signal. Both specs run: host.spec.ts (the admin UI) and
# consumer.spec.ts (the consumer page with the admin UI). Installs Chromium once, into
# Playwright's own cache (no sudo; CI installs its system libraries first).
web-e2e: web-admin-build web-consumer-build
	CGO_ENABLED=0 go test -c -o bin/e2e-harness ./cmd/local-server
	cd $(WEB) && npx playwright install chromium
	cd $(WEB) && LS_E2E_HARNESS=$(CURDIR)/bin/e2e-harness npm run e2e

# The old name of web-e2e.
web-admin-e2e: web-e2e

# Removes the built UIs (keeping each .gitkeep) and the generated API types.
clean-web:
	find $(ADMIN_DIST) -mindepth 1 ! -name .gitkeep -delete
	find $(CONSUMER_DIST) -mindepth 1 ! -name .gitkeep -delete
	rm -f $(WEB)/shared/src/api/schema.gen.ts

# The release build (PLT-C-003): the two web UIs, then one CGO_ENABLED=0 binary per OS and CPU in
# dist/ (darwin, linux, windows, each amd64 and arm64), named local-server_<os>_<arch>[.exe], and
# dist/SHA256SUMS with one "<hash>  <name>" line per binary, bare names, so that
# `cd dist && shasum -a 256 -c SHA256SUMS` (Linux: `sha256sum -c`) checks them. The release
# workflow runs this same recipe with VERSION set to the tag. The binaries are never stripped,
# re-signed or packed after the build (F-07, D-62): the attestation is a separate artifact.
release-build: web-admin-build web-consumer-build
	rm -rf dist
	mkdir -p dist
	for pair in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do \
		os=$${pair%/*}; arch=$${pair#*/}; ext=""; \
		if [ "$$os" = windows ]; then ext=".exe"; fi; \
		echo "building $$os/$$arch ($(VERSION))"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "-X main.version=$(VERSION)" \
			-o "dist/local-server_$${os}_$${arch}$$ext" ./cmd/local-server || exit 1; \
	done
	cd dist && { \
		if command -v sha256sum >/dev/null 2>&1; then sum="sha256sum"; else sum="shasum -a 256"; fi; \
		for f in local-server_*; do $$sum "$$f" || exit 1; done > SHA256SUMS; \
	}

# The local run of the release build: no upload and no attestation, only the files in dist/. Use
# `make release-dry VERSION=v0.0.0-dry` to give the binaries a version string of your choice.
release-dry: release-build
	@echo "release-dry: built the binaries and dist/SHA256SUMS locally in dist/; nothing was uploaded or attested"
