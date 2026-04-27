.PHONY: dev server daemon cli multica build test migrate-up migrate-down sqlc seed clean setup start stop check worktree-env setup-main start-main stop-main check-main setup-worktree start-worktree stop-worktree check-worktree db-up db-down db-reset selfhost selfhost-stop

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
MULTICA_APP_URL ?= $(FRONTEND_ORIGIN)
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
NEXT_PUBLIC_API_URL ?= http://localhost:$(PORT)
NEXT_PUBLIC_WS_URL ?= ws://localhost:$(PORT)/ws
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

# ---------- Self-hosting (Docker Compose) ----------

# One-command self-host: create env, start Docker Compose, wait for health
selfhost:
	@if [ ! -f .env ]; then \
		echo "==> Creating .env from .env.example..."; \
		cp .env.example .env; \
		JWT=$$(openssl rand -hex 32); \
		if [ "$$(uname)" = "Darwin" ]; then \
			sed -i '' "s/^JWT_SECRET=.*/JWT_SECRET=$$JWT/" .env; \
		else \
			sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$$JWT/" .env; \
		fi; \
		echo "==> Generated random JWT_SECRET"; \
	fi
	@echo "==> Starting Multica via Docker Compose..."
	docker compose -f docker-compose.selfhost.yml up -d --build
	@echo "==> Waiting for backend to be ready..."
	@for i in $$(seq 1 30); do \
		if curl -sf http://localhost:$${PORT:-8080}/health > /dev/null 2>&1; then \
			break; \
		fi; \
		sleep 2; \
	done
	@if curl -sf http://localhost:$${PORT:-8080}/health > /dev/null 2>&1; then \
		echo ""; \
		echo "✓ Multica is running!"; \
		echo "  Frontend: http://localhost:$${FRONTEND_PORT:-3000}"; \
		echo "  Backend:  http://localhost:$${PORT:-8080}"; \
		echo ""; \
		echo "Log in: configure RESEND_API_KEY in .env for email codes,"; \
		echo "        or set APP_ENV=development in .env (private networks only) to enable code 888888."; \
		echo ""; \
		echo "Next — install the CLI and connect your machine:"; \
		echo "  brew install multica-ai/tap/multica"; \
		echo "  multica setup self-host"; \
	else \
		echo ""; \
		echo "Services are still starting. Check logs:"; \
		echo "  docker compose -f docker-compose.selfhost.yml logs"; \
	fi

# Stop all Docker Compose self-host services
selfhost-stop:
	@echo "==> Stopping Multica services..."
	docker compose -f docker-compose.selfhost.yml down
	@echo "✓ All services stopped."

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

# Start all services (backend + frontend)
start:
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

# Stop all services
stop:
	$(REQUIRE_ENV)
	@echo "Stopping services..."
	@-lsof -ti:$(PORT) | xargs kill -9 2>/dev/null
	@-lsof -ti:$(FRONTEND_PORT) | xargs kill -9 2>/dev/null
	@case "$(DATABASE_URL)" in \
		""|*@localhost:*|*@localhost/*|*@127.0.0.1:*|*@127.0.0.1/*|*@\[::1\]:*|*@\[::1\]/*) \
			echo "✓ App processes stopped. Shared PostgreSQL is still running on localhost:$(POSTGRES_PORT)." ;; \
		*) \
			echo "✓ App processes stopped. Remote PostgreSQL was not affected." ;; \
	esac

# Full verification: typecheck + unit tests + Go tests + E2E
check:
	$(REQUIRE_ENV)
	@ENV_FILE="$(ENV_FILE)" bash scripts/check.sh

db-up:
	@$(COMPOSE) up -d postgres

db-down:
	@$(COMPOSE) down

# Drop + recreate the current env's database, then run all migrations.
# Use for a clean slate in local dev. Only affects the DB named in
# ENV_FILE (POSTGRES_DB); the shared postgres container and other
# worktree DBs are untouched. Refuses to run against a remote host.
db-reset:
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

worktree-env:
	@bash scripts/init-worktree-env.sh .env.worktree

setup-main:
	@$(MAKE) setup ENV_FILE=$(MAIN_ENV_FILE)

start-main:
	@$(MAKE) start ENV_FILE=$(MAIN_ENV_FILE)

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

stop-worktree:
	@$(MAKE) stop ENV_FILE=$(WORKTREE_ENV_FILE)

check-worktree:
	@ENV_FILE=$(WORKTREE_ENV_FILE) bash scripts/check.sh

# ---------- Individual commands ----------

# One-command dev: auto-setup env/deps/db/migrations, then start all services
dev:
	@bash scripts/dev.sh

# Go server only
server:
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/server

daemon:
	@$(MAKE) multica MULTICA_ARGS="daemon restart --profile local"

cli:
	@$(MAKE) multica MULTICA_ARGS="$(MULTICA_ARGS)"

multica:
	cd server && go run ./cmd/multica $(MULTICA_ARGS)

daemon-local-install:
	mkdir -p $(HOME)/.local/bin
	@VERSION="$$(git describe --tags --always --dirty 2>/dev/null || echo dev)"; \
	COMMIT="$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"; \
	DATE="$$(date -u '+%Y-%m-%dT%H:%M:%SZ')"; \
	cd server && go build -ldflags "-X main.version=$$VERSION -X main.commit=$$COMMIT -X main.date=$$DATE" -o ../dist/multica-local ./cmd/multica; \
	cp ../dist/multica-local $(HOME)/.local/bin/multica; \
	echo "Installed local daemon CLI to $(HOME)/.local/bin/multica ($$VERSION)"

daemon-dist-all: daemon-linux-build daemon-windows-build

daemon-linux-build:
	@VERSION="$$(git describe --tags --always --dirty 2>/dev/null || echo dev)"; \
	COMMIT="$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"; \
	DATE="$$(date -u '+%Y-%m-%dT%H:%M:%SZ')"; \
	cd server && CGO_ENABLED=0 go build -ldflags "-X main.version=$$VERSION -X main.commit=$$COMMIT -X main.date=$$DATE" -o ../dist/linux/multica ./cmd/multica; \
	cp ../dist/linux/multica $(HOME)/.local/share/multica/dist/linux/multica; \
	echo "Built Linux CLI to dist/linux/multica ($$VERSION)"

daemon-windows-build:
	@VERSION="$$(git describe --tags --always --dirty 2>/dev/null || echo dev)"; \
	COMMIT="$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"; \
	DATE="$$(date -u '+%Y-%m-%dT%H:%M:%SZ')"; \
	mkdir -p ../dist/windows; \
	cd server && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.version=$$VERSION -X main.commit=$$COMMIT -X main.date=$$DATE" -o ../dist/windows/multica.exe ./cmd/multica; \
	cp ../dist/windows/multica.exe $(HOME)/.local/share/multica/dist/windows/multica.exe; \
	echo "Built Windows CLI to dist/windows/multica.exe ($$VERSION)"

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

build:
	cd server && go build -o bin/server ./cmd/server
	cd server && go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)" -o bin/multica ./cmd/multica
	cd server && go build -o bin/migrate ./cmd/migrate

test:
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate up
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
