.PHONY: help makehelp dev server daemon cli multica build test migrate-up migrate-down sqlc seed clean setup start stop check worktree-env setup-main start-main stop-main check-main setup-worktree start-worktree stop-worktree check-worktree remove-worktree db-up db-down db-drop db-reset selfhost selfhost-build selfhost-stop up down status list destroy gc env-exec api-dev web-dev desktop-dev

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
PORT := $(or $(BACKEND_PORT),$(API_PORT),$(SERVER_PORT),$(PORT),8080)
ifeq ($(origin MULTICA_PUBLIC_URL), undefined)
MULTICA_PUBLIC_URL := http://localhost:$(PORT)
endif
FRONTEND_PORT ?= 3000
FRONTEND_ORIGIN ?= http://localhost:$(FRONTEND_PORT)
MULTICA_APP_URL ?= $(FRONTEND_ORIGIN)
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
NEXT_PUBLIC_API_URL ?= http://localhost:$(PORT)
NEXT_PUBLIC_WS_URL ?= ws://localhost:$(PORT)/ws
GOOGLE_REDIRECT_URI ?= $(FRONTEND_ORIGIN)/auth/callback
MULTICA_SERVER_URL ?= ws://localhost:$(PORT)/ws
LOCAL_UPLOAD_BASE_URL ?= http://localhost:$(PORT)

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

# Self-hosting requires the Docker Compose CLI plugin (`docker compose`).
# The self-host compose files use compose-spec syntax (top-level `name:`, no
# `version:`) that the legacy v1 `docker-compose` standalone cannot parse, so we
# fail early with an actionable message instead of a cryptic CLI parse error
# (e.g. "unknown shorthand flag: 'f' in -f") when the plugin is missing or v1.
# Keep the message short and OS-agnostic: per-OS install steps belong in docs.
define REQUIRE_COMPOSE
	@if ! compose_version=$$($(COMPOSE) version --short 2>/dev/null); then \
		echo "Docker Compose ('docker compose') was not found."; \
		echo "Self-hosting requires the Compose CLI plugin; legacy 'docker-compose' v1 is not supported."; \
		echo "Install Docker Compose from https://docs.docker.com/compose/install/ and verify with: docker compose version"; \
		exit 1; \
	fi; \
	case "$$compose_version" in \
		1.*|v1.*) \
			echo "'$(COMPOSE)' is legacy Docker Compose v1 ($$compose_version)."; \
			echo "Self-hosting requires the Compose CLI plugin; legacy 'docker-compose' v1 is not supported."; \
			echo "Install Docker Compose from https://docs.docker.com/compose/install/ and verify with: docker compose version"; \
			exit 1; \
			;; \
	esac
endef

# Default target changed from selfhost to help: bare `make` now prints this help
# instead of launching a full Docker Compose build, which is safer for onboarding.
.DEFAULT_GOAL := help

##@ Help

help: ## Show available make targets and common local workflows
	@awk 'BEGIN {FS = ":.*## "; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nQuick start:\n  \033[36mmake up\033[0m           Start this checkout'"'"'s environment (C=api,web,daemon,desktop)\n  \033[36mmake status\033[0m       Show what is running, and prove it is yours\n  \033[36mmake down\033[0m         Stop it again, keeping the database\n  \033[36mmake check\033[0m        Run the full local verification pipeline\n\nCheckout modes:\n  Main checkout uses \033[36m.env\033[0m\n  Worktrees use \033[36m.env.worktree\033[0m (generate with \033[36mmake worktree-env\033[0m)\n\n"} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5); next} \
		/^[a-zA-Z0-9_.-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

makehelp: help ## Alias for `make help`

# ---------- Self-hosting (Docker Compose) ----------
##@ Self-hosting

selfhost: ## Create .env if needed, then pull and start the official self-hosted images
	$(REQUIRE_COMPOSE)
	@if [ ! -f .env ]; then \
		echo "==> Creating .env from .env.example..."; \
		cp .env.example .env; \
		JWT=$$(openssl rand -hex 32); \
		PGPASS=$$(openssl rand -hex 24); \
		VCSKEY=$$(openssl rand -base64 32); \
		if [ "$$(uname)" = "Darwin" ]; then \
			sed -i '' "s/^JWT_SECRET=.*/JWT_SECRET=$$JWT/" .env; \
			sed -i '' "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$$PGPASS/" .env; \
			sed -i '' -E "s#^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)#\1$$PGPASS\2#" .env; \
			sed -i '' "s#^MULTICA_VCS_SECRET_KEY=.*#MULTICA_VCS_SECRET_KEY=$$VCSKEY#" .env; \
		else \
			sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$$JWT/" .env; \
			sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$$PGPASS/" .env; \
			sed -i -E "s#^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)#\1$$PGPASS\2#" .env; \
			sed -i "s#^MULTICA_VCS_SECRET_KEY=.*#MULTICA_VCS_SECRET_KEY=$$VCSKEY#" .env; \
		fi; \
		echo "==> Generated random JWT_SECRET, POSTGRES_PASSWORD, and MULTICA_VCS_SECRET_KEY"; \
	fi
	@echo "==> Pulling official Multica images..."
	@if ! $(COMPOSE) -f docker-compose.selfhost.yml pull; then \
		echo ""; \
		echo "Official images for tag '$${MULTICA_IMAGE_TAG:-latest}' are not published yet."; \
		echo "If this is before the first GHCR release, build from the current checkout:"; \
		echo "  make selfhost-build"; \
		exit 1; \
	fi
	@echo "==> Starting Multica via Docker Compose..."
	$(COMPOSE) -f docker-compose.selfhost.yml up -d
	@bash scripts/selfhost-wait.sh official

selfhost-build: ## Build backend/web from the current checkout and start the self-hosted stack
	$(REQUIRE_COMPOSE)
	@if [ ! -f .env ]; then \
		echo "==> Creating .env from .env.example..."; \
		cp .env.example .env; \
		JWT=$$(openssl rand -hex 32); \
		PGPASS=$$(openssl rand -hex 24); \
		VCSKEY=$$(openssl rand -base64 32); \
		if [ "$$(uname)" = "Darwin" ]; then \
			sed -i '' "s/^JWT_SECRET=.*/JWT_SECRET=$$JWT/" .env; \
			sed -i '' "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$$PGPASS/" .env; \
			sed -i '' -E "s#^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)#\1$$PGPASS\2#" .env; \
			sed -i '' "s#^MULTICA_VCS_SECRET_KEY=.*#MULTICA_VCS_SECRET_KEY=$$VCSKEY#" .env; \
		else \
			sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$$JWT/" .env; \
			sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$$PGPASS/" .env; \
			sed -i -E "s#^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)#\1$$PGPASS\2#" .env; \
			sed -i "s#^MULTICA_VCS_SECRET_KEY=.*#MULTICA_VCS_SECRET_KEY=$$VCSKEY#" .env; \
		fi; \
		echo "==> Generated random JWT_SECRET, POSTGRES_PASSWORD, and MULTICA_VCS_SECRET_KEY"; \
	fi
	@echo "==> Building Multica from the current checkout..."
	$(COMPOSE) -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build
	@bash scripts/selfhost-wait.sh build

selfhost-stop: ## Stop the self-hosted Docker Compose stack
	$(REQUIRE_COMPOSE)
	@echo "==> Stopping Multica services..."
	$(COMPOSE) -f docker-compose.selfhost.yml down
	@echo "✓ All services stopped."

# ---------- Environments ----------
##@ Environments

# One verb per lifecycle step, shared by humans and agents. C= picks the
# components; ARGS= forwards anything else to the script.
#
#   make up                     api + web
#   make up C=api,web,daemon    add the agent daemon
#   make up C=desktop           Electron against this environment's backend
#   make up ARGS=--ephemeral    agent-owned, expires, collected by `make gc`

up: ## Start this checkout's environment (C=api,web,daemon,desktop; default api,web)
	@bash scripts/dev-env.sh up $(if $(C),--components $(C)) $(ARGS)

down: ## Stop this environment's processes, keeping its database and profile
	@bash scripts/dev-env.sh down $(ARGS)

status: ## Show what is running for this environment, with proof of identity
	@bash scripts/dev-env.sh status $(ARGS)

list: ## List every registered development environment on this machine
	@bash scripts/dev-env.sh list $(ARGS)

destroy: ## Stop this environment, drop its database and profile, free its slot
	@bash scripts/dev-env.sh destroy $(ARGS)

gc: ## Collect environments whose directory is gone or whose TTL expired
	@bash scripts/dev-env.sh gc $(ARGS)

env-exec: ## Run a command with this environment's variables (ARGS="-- pnpm dev:desktop")
	@bash scripts/dev-env.sh exec $(ARGS)

# Single-component entry points. No database preflight: `up` has already proven
# the database is reachable before it launches anything, and repeating the
# check here would make each component slower to restart on its own.
# The commit ldflag is what makes /health's identity useful: without it every
# environment reports "unknown" and cannot prove which revision it is serving.
api-dev: ## Run only the Go backend for the current env file
	cd server && go run -ldflags "-X main.commit=$(COMMIT)" ./cmd/server

web-dev: ## Run only the Next.js dev server for the current env file
	pnpm dev:web

desktop-dev: ## Run only the Electron desktop app for the current env file
	pnpm dev:desktop

# ---------- One-click commands ----------
##@ One-click

setup: ## Prepare the current checkout from its env file: install deps, ensure DB, run migrations
	$(REQUIRE_ENV)
	@echo "==> Using env file: $(ENV_FILE)"
	@echo "==> Installing dependencies..."
	pnpm install
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@echo "==> Running migrations..."
	cd server && go run ./cmd/migrate up
	@echo ""
	@echo "✓ Setup complete! Run 'make start' to launch the app."

start: ## Start backend and frontend for the current checkout and run migrations first
	$(REQUIRE_ENV)
	@echo "Using env file: $(ENV_FILE)"
	@echo "Backend: http://localhost:$(PORT)"
	@echo "Frontend: http://localhost:$(FRONTEND_PORT)"
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@echo "Running migrations..."
	cd server && go run ./cmd/migrate up
	@echo "Starting backend and frontend..."
	@trap 'kill 0' EXIT; \
		(cd server && go run ./cmd/server) & \
		pnpm dev:web & \
		wait

stop: ## Stop backend and frontend processes for the current checkout
	$(REQUIRE_ENV)
	@echo "Stopping services..."
	@-lsof -nP -iTCP:$(PORT) -sTCP:LISTEN -t | xargs kill -9 2>/dev/null
	@-lsof -nP -iTCP:$(FRONTEND_PORT) -sTCP:LISTEN -t | xargs kill -9 2>/dev/null
	@case "$(DATABASE_URL)" in \
		""|*@localhost:*|*@localhost/*|*@127.0.0.1:*|*@127.0.0.1/*|*@\[::1\]:*|*@\[::1\]/*) \
			echo "✓ App processes stopped. Shared PostgreSQL is still running on localhost:$(POSTGRES_PORT)." ;; \
		*) \
			echo "✓ App processes stopped. Remote PostgreSQL was not affected." ;; \
	esac

check: ## Run typecheck, TS tests, Go tests, and Playwright E2E for the current checkout
	$(REQUIRE_ENV)
	@ENV_FILE="$(ENV_FILE)" bash scripts/check.sh

db-up: ## Start the shared PostgreSQL container used by main and worktrees
	@$(COMPOSE) up -d postgres

db-down: ## Stop the shared PostgreSQL container without removing its Docker volume
	@$(COMPOSE) down

db-drop: ## Permanently drop the current env's local database after confirmation
	$(REQUIRE_ENV)
	@status=0; bash scripts/drop-database.sh "$(ENV_FILE)" || status=$$?; \
		if [ "$$status" -eq 2 ]; then exit 0; fi; \
		exit "$$status"

# Drop + recreate the current env's database, then run all migrations.
# Use for a clean slate in local dev. Only affects the DB named in
# ENV_FILE (POSTGRES_DB); the shared postgres container and other
# worktree DBs are untouched. Refuses to run against a remote host.
db-reset: ## Drop and recreate the current env's database, then re-run all migrations
	$(REQUIRE_ENV)
	@case "$(DATABASE_URL)" in \
		""|*@localhost:*|*@localhost/*|*@127.0.0.1:*|*@127.0.0.1/*|*@\[::1\]:*|*@\[::1\]/*) ;; \
		*) echo "Refusing to reset: DATABASE_URL points at a remote host."; exit 1 ;; \
	esac
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@echo "==> Dropping and recreating database '$(POSTGRES_DB)'..."
	@$(COMPOSE) exec -T postgres psql -U $(POSTGRES_USER) -d postgres -v ON_ERROR_STOP=1 \
		-c "DROP DATABASE IF EXISTS \"$(POSTGRES_DB)\" WITH (FORCE);" \
		-c "CREATE DATABASE \"$(POSTGRES_DB)\";"
	@echo "==> Running migrations..."
	cd server && go run ./cmd/migrate up
	@echo ""
	@echo "✓ Database '$(POSTGRES_DB)' reset. Run 'make start' to launch the app."

worktree-env: ## Generate .env.worktree with a unique DB name and app ports for this worktree
	@bash scripts/init-worktree-env.sh .env.worktree

setup-main: ## Prepare the main checkout using .env
	@$(MAKE) setup ENV_FILE=$(MAIN_ENV_FILE)

start-main: ## Start the main checkout using .env
	@$(MAKE) start ENV_FILE=$(MAIN_ENV_FILE)

stop-main: ## Stop the main checkout processes defined by .env
	@$(MAKE) stop ENV_FILE=$(MAIN_ENV_FILE)

check-main: ## Run the full verification pipeline for the main checkout
	@ENV_FILE=$(MAIN_ENV_FILE) bash scripts/check.sh

setup-worktree: ## Ensure .env.worktree exists, then prepare this worktree
	@if [ ! -f "$(WORKTREE_ENV_FILE)" ]; then \
		echo "==> Generating $(WORKTREE_ENV_FILE) with unique ports..."; \
		bash scripts/init-worktree-env.sh $(WORKTREE_ENV_FILE); \
	else \
		echo "==> Using existing $(WORKTREE_ENV_FILE)"; \
	fi
	@$(MAKE) setup ENV_FILE=$(WORKTREE_ENV_FILE)

start-worktree: ## Start this worktree using .env.worktree
	@$(MAKE) start ENV_FILE=$(WORKTREE_ENV_FILE)

stop-worktree: ## Stop this worktree's backend and frontend processes
	@$(MAKE) stop ENV_FILE=$(WORKTREE_ENV_FILE)

check-worktree: ## Run the full verification pipeline for this worktree
	@ENV_FILE=$(WORKTREE_ENV_FILE) bash scripts/check.sh

remove-worktree: ## Drop a linked worktree's database, then remove it (WORKTREE=path)
	@bash scripts/remove-worktree.sh "$(WORKTREE)"

# ---------- Individual commands ----------
##@ Individual commands

dev: ## Bootstrap this checkout end-to-end: create env if needed, ensure DB, migrate, start services
	@bash scripts/dev.sh

server: ## Run only the Go server for the current checkout
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/server

daemon: ## Restart the local agent daemon using the CLI's stored auth/session
	@$(MAKE) multica MULTICA_ARGS="daemon restart --profile local"

cli: ## Run the multica CLI with ARGS or MULTICA_ARGS from source
	@$(MAKE) multica MULTICA_ARGS="$(MULTICA_ARGS)"

multica: ## Run the multica CLI entrypoint directly from the Go source tree
	cd server && go run -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)" ./cmd/multica $(MULTICA_ARGS)

VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
# Windows will not execute an extensionless binary, so a source build there has
# to name its outputs the way the target platform expects — otherwise the CLI
# builds fine and then fails to re-exec itself as a daemon (#7255). GOOS reaches
# a build two ways: as an environment variable (`GOOS=windows make build`) and
# as a Make variable (`make build GOOS=windows`). The top-level `export` sends
# both forms to the recipe, so `go build` honors both and the suffix has to as
# well; `$(GOOS)` covers the Make-variable form, which a parse-time
# `go env GOOS` cannot see. Target-specific so only `build` pays for the probe:
# a global assignment runs `go env` on every target — `export` expands even a
# recursive one — which prints `go: Command not found` on frontend-only
# checkouts with no Go toolchain installed.
build: EXE = $(if $(filter windows,$(or $(GOOS),$(shell go env GOOS))),.exe,)
build: ## Build the server, CLI, and migrate binaries into server/bin
	cd server && go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/server$(EXE) ./cmd/server
	cd server && go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)" -o bin/multica$(EXE) ./cmd/multica
	cd server && go build -o bin/migrate$(EXE) ./cmd/migrate

test: ## Run Go tests after ensuring the target DB exists and migrations are applied
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate up
	bash scripts/test-go.sh --race

# Database
##@ Database

migrate-up: ## Create the target DB if needed, then apply database migrations
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate up

migrate-down: ## Create the target DB if needed, then roll back database migrations
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate down

sqlc: ## Regenerate sqlc code
	cd server && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

# Cleanup
##@ Cleanup

clean: ## Remove build caches, generated binaries, and temp files
	rm -rf server/bin server/tmp
	rm -rf apps/*/.next apps/*/.source apps/*/.expo
	rm -rf apps/*/out apps/*/dist apps/*/dist-electron packages/*/dist
	rm -rf .turbo apps/*/.turbo packages/*/.turbo
	rm -rf apps/*/*.tsbuildinfo packages/*/*.tsbuildinfo
	@echo "✓ Clean complete."
