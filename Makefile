.PHONY: dev daemon cli multica build release-build release-build-multi release-package release-package-multi test migrate-up migrate-down sqlc seed clean setup start start-air stop check doctor-agent-loop worktree-env init-worktree-env list-worktree-resources destroy-worktree remove-worktree setup-main start-main start-air-main stop-main check-main setup-worktree guard-worktree start-worktree start-air-worktree stop-worktree check-worktree db-up db-down

MAIN_ENV_FILE ?= .env
WORKTREE_ENV_FILE ?= .env.worktree
ENV_FILE ?= $(if $(wildcard $(MAIN_ENV_FILE)),$(MAIN_ENV_FILE),$(if $(wildcard $(WORKTREE_ENV_FILE)),$(WORKTREE_ENV_FILE),$(MAIN_ENV_FILE)))

ifneq ($(wildcard $(ENV_FILE)),)
include $(ENV_FILE)
endif

POSTGRES_DB ?= multica
POSTGRES_USER ?= multica
POSTGRES_PASSWORD ?= multica
POSTGRES_PORT ?= 5432
PORT ?= 8080
FRONTEND_PORT ?= 3000
FRONTEND_ORIGIN ?= http://localhost:$(FRONTEND_PORT)
WORKSPACE_SITE_ORIGIN ?= $(FRONTEND_ORIGIN)
MULTICA_APP_URL ?= $(FRONTEND_ORIGIN)
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
VITE_API_URL ?=
VITE_WS_URL ?=
WORKSPACE_DIST_DIR ?= ../apps/workspace/dist
GOOGLE_REDIRECT_URI ?= $(FRONTEND_ORIGIN)/auth/callback
MULTICA_SERVER_URL ?= ws://localhost:$(PORT)/ws

export

MULTICA_ARGS ?= $(ARGS)

COMPOSE := docker compose

define REQUIRE_ENV
	@if [ ! -f "$(ENV_FILE)" ]; then \
		echo "Missing env file: $(ENV_FILE)"; \
		echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."; \
		exit 1; \
	fi
endef

define REQUIRE_WORKTREE_READY_IF_NEEDED
	@if [ "$$(basename "$(ENV_FILE)")" = "$$(basename "$(WORKTREE_ENV_FILE)")" ] || grep -q '^MULTICA_ENV_KIND=worktree$$' "$(ENV_FILE)" 2>/dev/null; then \
		bash scripts/check-worktree-ready.sh "$(ENV_FILE)" runtime; \
	fi
endef

# ---------- One-click commands ----------

# First-time setup: install deps, start DB, run migrations
setup:
	$(REQUIRE_ENV)
	@echo "==> Using env file: $(ENV_FILE)"
	@echo "==> Installing dependencies..."
	pnpm install
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@echo "==> Running migrations..."
	cd server && go run ./cmd/migrate up
	@echo ""
	@echo "✓ Setup complete! Run 'make start' to launch the app."

# Start app services (backend + workspace)
start:
	$(REQUIRE_ENV)
	$(REQUIRE_WORKTREE_READY_IF_NEEDED)
	@echo "Using env file: $(ENV_FILE)"
	@echo "Backend/API: http://localhost:$(PORT)"
	@echo "Workspace: http://localhost:$(FRONTEND_PORT)"
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@bash scripts/check-dev-ports.sh "$(ENV_FILE)" backend workspace
	@echo "Starting backend and workspace SPA..."
	@trap 'kill 0' EXIT; \
		(cd server && go run ./cmd/server) & \
		pnpm dev:workspace & \
		wait

start-air:
	$(REQUIRE_ENV)
	$(REQUIRE_WORKTREE_READY_IF_NEEDED)
	@ENV_FILE="$(ENV_FILE)" bash scripts/dev-air.sh

# Stop all services
stop:
	$(REQUIRE_ENV)
	@echo "Stopping services..."
	@-lsof -ti:$(PORT) | xargs kill -9 2>/dev/null
	@-lsof -ti:$(FRONTEND_PORT) | xargs kill -9 2>/dev/null
	@echo "✓ App processes stopped. PostgreSQL is still running on the configured local port."

# Full verification: typecheck + unit tests + Go tests + E2E
check:
	$(REQUIRE_ENV)
	$(REQUIRE_WORKTREE_READY_IF_NEEDED)
	@ENV_FILE="$(ENV_FILE)" bash scripts/check.sh

# Agent runtime loop smoke check: schema + runtime + claim + task token + revoke
doctor-agent-loop:
	$(REQUIRE_ENV)
	$(REQUIRE_WORKTREE_READY_IF_NEEDED)
	@ENV_FILE="$(ENV_FILE)" bash scripts/doctor-agent-loop.sh

db-up:
	@$(COMPOSE) up -d postgres

db-down:
	@$(COMPOSE) down

worktree-env:
	@echo "==> 'make worktree-env' now runs the full worktree setup flow."
	@$(MAKE) setup-worktree

init-worktree-env:
	@bash scripts/init-worktree-env.sh .env.worktree

list-worktree-resources:
	@bash scripts/list-worktree-resources.sh "$(WORKTREE_ENV_FILE)"

destroy-worktree:
	@bash scripts/destroy-worktree.sh "$(WORKTREE_ENV_FILE)"

remove-worktree:
	@if [ -z "$(WORKTREE_PATH)" ]; then \
		echo "Set WORKTREE_PATH=/absolute/or/relative/path/to/the-worktree you want to remove."; \
		echo "Example: make remove-worktree WORKTREE_PATH=../multica-feature FORCE=1"; \
		exit 1; \
	fi
	@bash scripts/remove-worktree.sh "$(WORKTREE_PATH)"

setup-main:
	@$(MAKE) setup ENV_FILE=$(MAIN_ENV_FILE)

start-main:
	@$(MAKE) start ENV_FILE=$(MAIN_ENV_FILE)

start-air-main:
	@$(MAKE) start-air ENV_FILE=$(MAIN_ENV_FILE)

stop-main:
	@$(MAKE) stop ENV_FILE=$(MAIN_ENV_FILE)

check-main:
	@ENV_FILE=$(MAIN_ENV_FILE) bash scripts/check.sh

setup-worktree:
	@if [ ! -f "$(WORKTREE_ENV_FILE)" ]; then \
		echo "==> Generating $(WORKTREE_ENV_FILE) with unique ports..."; \
		bash scripts/init-worktree-env.sh $(WORKTREE_ENV_FILE); \
	else \
		echo "==> Using existing $(WORKTREE_ENV_FILE)"; \
	fi
	@if ! grep -q '^MULTICA_ENV_KIND=worktree$$' "$(WORKTREE_ENV_FILE)" 2>/dev/null; then \
		printf '\nMULTICA_ENV_KIND=worktree\n' >> "$(WORKTREE_ENV_FILE)"; \
	fi
	@if ! grep -q '^WORKTREE_NAME=' "$(WORKTREE_ENV_FILE)" 2>/dev/null; then \
		printf 'WORKTREE_NAME=%s\n' "$$(basename "$$PWD")" >> "$(WORKTREE_ENV_FILE)"; \
	fi
	@if ! grep -q '^WORKTREE_ROOT=' "$(WORKTREE_ENV_FILE)" 2>/dev/null; then \
		printf 'WORKTREE_ROOT=%s\n' "$$PWD" >> "$(WORKTREE_ENV_FILE)"; \
	fi
	@$(MAKE) setup ENV_FILE=$(WORKTREE_ENV_FILE)
	@bash scripts/check-worktree-ready.sh "$(WORKTREE_ENV_FILE)" setup

guard-worktree:
	@bash scripts/check-worktree-ready.sh "$(WORKTREE_ENV_FILE)" runtime

start-worktree: guard-worktree
	@$(MAKE) start ENV_FILE=$(WORKTREE_ENV_FILE)

start-air-worktree: guard-worktree
	@$(MAKE) start-air ENV_FILE=$(WORKTREE_ENV_FILE)

stop-worktree:
	@$(MAKE) stop ENV_FILE=$(WORKTREE_ENV_FILE)

check-worktree: guard-worktree
	@ENV_FILE=$(WORKTREE_ENV_FILE) bash scripts/check.sh

# ---------- Individual commands ----------

# Go server
dev:
	$(REQUIRE_ENV)
	$(REQUIRE_WORKTREE_READY_IF_NEEDED)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/server

daemon:
	@$(MAKE) multica MULTICA_ARGS="daemon"

cli:
	@$(MAKE) multica MULTICA_ARGS="$(MULTICA_ARGS)"

multica:
	cd server && go run ./cmd/multica $(MULTICA_ARGS)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

build:
	cd server && go build -o bin/server ./cmd/server
	cd server && go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/multica ./cmd/multica

release-build:
	bash scripts/build-release.sh

release-build-multi:
	RELEASE_TARGETS="$(RELEASE_TARGETS)" bash scripts/build-release-multi.sh

release-package:
	bash scripts/package-release.sh $(RELEASE_OUTPUT_DIR)

release-package-multi:
	RELEASE_TARGETS="$(RELEASE_TARGETS)" SKIP_BUILD="$(SKIP_BUILD)" bash scripts/package-release-multi.sh $(RELEASE_OUTPUT_DIR)

test:
	$(REQUIRE_ENV)
	$(REQUIRE_WORKTREE_READY_IF_NEEDED)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go test ./...

# Database
migrate-up:
	$(REQUIRE_ENV)
	$(REQUIRE_WORKTREE_READY_IF_NEEDED)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate up

migrate-down:
	$(REQUIRE_ENV)
	$(REQUIRE_WORKTREE_READY_IF_NEEDED)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate down

sqlc:
	cd server && sqlc generate

# Cleanup
clean:
	rm -rf server/bin server/tmp
