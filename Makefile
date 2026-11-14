# Builds the web UIs, then the Go binary (req_001 D-151, D-152). A plain `go build ./...` needs
# no npm: it embeds only web/admin/dist/.gitkeep and serves a one-page stub instead of the UI.
# The web toolchain is Node.js 24 LTS (web/.nvmrc) with npm workspaces under web/.

WEB := web
ADMIN_DIST := $(WEB)/admin/dist

.PHONY: build web-deps web-gen web-admin-build web-admin-test clean-web

# The binary with the built admin UI inside.
build: web-admin-build
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

# Removes the built admin UI (keeping .gitkeep) and the generated API types.
clean-web:
	find $(ADMIN_DIST) -mindepth 1 ! -name .gitkeep -delete
	rm -f $(WEB)/shared/src/api/schema.gen.ts
