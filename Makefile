.PHONY: dev daemon cli multica build release-build release-build-multi release-package release-package-multi test migrate-up migrate-down sqlc seed clean setup start start-air stop check worktree-env setup-main start-main start-air-main stop-main check-main setup-worktree start-worktree start-air-worktree stop-worktree check-worktree db-up db-down

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
MARKETING_PORT ?= 3001
FRONTEND_ORIGIN ?= http://localhost:$(FRONTEND_PORT)
WORKSPACE_SITE_ORIGIN ?= $(FRONTEND_ORIGIN)
MARKETING_SITE_ORIGIN ?= http://localhost:$(MARKETING_PORT)
MULTICA_APP_URL ?= $(FRONTEND_ORIGIN)
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
NEXT_PUBLIC_API_URL ?= http://localhost:$(PORT)
NEXT_PUBLIC_WS_URL ?= ws://localhost:$(PORT)/ws
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
	@echo "Using env file: $(ENV_FILE)"
	@echo "Backend/API: http://localhost:$(PORT)"
	@echo "Workspace: http://localhost:$(FRONTEND_PORT)"
	@bash scripts/check-postgres.sh "$(ENV_FILE)"
	@bash scripts/check-dev-ports.sh "$(ENV_FILE)" backend workspace
	@echo "Starting backend and workspace SPA..."
# pnpm dev:marketing & \
	@trap 'kill 0' EXIT; \
		(cd server && go run ./cmd/server) & \
		pnpm dev:workspace & \
		wait

start-air:
	$(REQUIRE_ENV)
	@ENV_FILE="$(ENV_FILE)" bash scripts/dev-air.sh

# Stop all services
stop:
	$(REQUIRE_ENV)
	@echo "Stopping services..."
	@-lsof -ti:$(PORT) | xargs kill -9 2>/dev/null
	@-lsof -ti:$(FRONTEND_PORT) | xargs kill -9 2>/dev/null
	@-lsof -ti:$(MARKETING_PORT) | xargs kill -9 2>/dev/null
	@echo "✓ App processes stopped. Shared PostgreSQL is still running on localhost:5432."

# Full verification: typecheck + unit tests + Go tests + E2E
check:
	$(REQUIRE_ENV)
	@ENV_FILE="$(ENV_FILE)" bash scripts/check.sh

db-up:
	@$(COMPOSE) up -d postgres

db-down:
	@$(COMPOSE) down

worktree-env:
	@bash scripts/init-worktree-env.sh .env.worktree

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
	@$(MAKE) setup ENV_FILE=$(WORKTREE_ENV_FILE)

start-worktree:
	@$(MAKE) start ENV_FILE=$(WORKTREE_ENV_FILE)

start-air-worktree:
	@$(MAKE) start-air ENV_FILE=$(WORKTREE_ENV_FILE)

stop-worktree:
	@$(MAKE) stop ENV_FILE=$(WORKTREE_ENV_FILE)

check-worktree:
	@ENV_FILE=$(WORKTREE_ENV_FILE) bash scripts/check.sh

# ---------- Individual commands ----------

# Go server
dev:
	$(REQUIRE_ENV)
	@bash scripts/check-postgres.sh "$(ENV_FILE)"
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
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go test ./...

# Database
migrate-up:
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate up

migrate-down:
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate down

sqlc:
	cd server && sqlc generate

# Cleanup
clean:
	rm -rf server/bin server/tmp
