#SHELL := /bin/bash

# COLORS
RED  := $(shell tput -Txterm setaf 1)
GREEN  := $(shell tput -Txterm setaf 2)
YELLOW := $(shell tput -Txterm setaf 3)
WHITE  := $(shell tput -Txterm setaf 7)
RESET  := $(shell tput -Txterm sgr0)

APP_PORT ?= 24275
TEMPL_PROXY_PORT ?= 9089

TAB_CHAR_NUM=20
user ?= ubuntu
binary_name ?= code_scout
service_name ?= code_scout

# Build identity, injected into package app. Defined once because there are
# three build paths (build, deploy, and the Dockerfile) and they had already
# drifted apart: the Dockerfile was passing a build arg named VERSION into a
# variable named BranchName. The version itself is NOT here — it is a constant
# in app/version.go, so a binary cannot be built without one.
#
# The branch and the dirty-file list used to be injected too. Nothing read
# either, and the dirty list piped `git status --porcelain` — arbitrary
# filenames, unquoted, newline separated — straight into a linker flag.
ldflags = -X 'github.com/getcodescout/code_scout/app.Commit=$(shell git rev-parse --short HEAD)' \
          -X 'github.com/getcodescout/code_scout/app.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)'

# Local settings live in .env (gitignored), so development needs no
# /etc/code-scout.conf. Anything already set in the environment wins.
-include .env
export

# Superuser for `make db`. Homebrew creates a role named after your macOS user;
# most Linux packages use `postgres`, so override there:
#   make db pg_super=postgres
pg_super ?= $(USER)

.PHONY: build build-local templ notify-templ-proxy run dev db env test test-e2e test-e2e-headed test-sdk-e2e screenshots

# Where the Flutter SDK is checked out. It is a separate repository, so this is
# the one place that assumes the two sit side by side.
sdk_dir ?= ../code_scout_flutter

## Show help
help:
	@echo ''
	@echo 'Usage:'
	@echo '  ${YELLOW}make${RESET} ${GREEN}<target>${RESET}'
	@echo ''
	@echo 'Targets:'
	@awk '/^[a-zA-Z\-\_0-9]+:/ { \
		helpMessage = match(lastLine, /^## (.*)/); \
		if (helpMessage) { \
			helpCommand = substr($$1, 0, index($$1, ":")-1); \
			helpMessage = substr(lastLine, RSTART + 3, RLENGTH); \
			printf "  ${YELLOW}%-$(TAB_CHAR_NUM)s${RESET} ${GREEN}%s${RESET}\n", helpCommand, helpMessage; \
		} \
	} \
	{ lastLine = $$0 }' $(MAKEFILE_LIST)

## Build the project for current OS & Arch
build:
	@ echo "-> Building project..."
	@ GOOS=linux GOARCH=amd64 go build -o ./bin/${binary_name} -ldflags="$(ldflags)" .
	@ echo "-> Done. ✓"

## Run the tests (integration tests skip unless CS_TEST_DB is set)
test:
	@ echo "-> Running tests..."
	@ go test ./...
	@ echo "-> Done.  ✓"

## Run tests including the ones needing a real Postgres
test-all:
	@ set -a; [ -f .env ] && . ./.env; set +a; \
	  psql -U $(pg_super) -d postgres -tAc \
	    "SELECT 1 FROM pg_database WHERE datname='$$CS_DB_NAME_test'" >/dev/null 2>&1; \
	  psql -U $(pg_super) -d postgres -tAc \
	    "SELECT 1 FROM pg_database WHERE datname='code_scout_test'" | grep -q 1 \
	    || psql -U $(pg_super) -d postgres -q -c "CREATE DATABASE code_scout_test OWNER $$CS_DB_USER;"; \
	  CS_TEST_DB="host=$$CS_DB_HOST port=$$CS_DB_PORT user=$$CS_DB_USER password=$$CS_DB_PASSWORD dbname=code_scout_test sslmode=disable" \
	    go test ./...

## Run the browser tests (needs `npm install` and Google Chrome)
##
## Everything runs against a throwaway database on a throwaway port, so this
## never touches the database you develop against. The trap cleans up even when
## a test fails or you interrupt it. Test files run one at a time because they
## share that server, and in parallel two of them both see a fresh instance and
## race to register the first account.
test-e2e:
	@ set -a; [ -f .env ] && . ./.env; set +a; \
	  set -e; \
	  port=24283; db=code_scout_e2e; \
	  echo "-> Building the server..."; \
	  go build -o ./bin/code_scout_e2e . ; \
	  psql -U $(pg_super) -d postgres -q -c "DROP DATABASE IF EXISTS $$db;"; \
	  psql -U $(pg_super) -d postgres -q -c "CREATE DATABASE $$db OWNER $$CS_DB_USER;"; \
	  CS_DB_NAME=$$db CS_PORT=$$port ./bin/code_scout_e2e > /tmp/code_scout_e2e.log 2>&1 & \
	  server=$$!; \
	  trap 'kill $$server 2>/dev/null; psql -U $(pg_super) -d postgres -q -c "DROP DATABASE IF EXISTS $$db;" >/dev/null 2>&1' EXIT; \
	  echo "-> Waiting for the server on :$$port..."; \
	  for i in $$(seq 1 40); do \
	    curl -sf "http://localhost:$$port/healthz" >/dev/null && break; \
	    sleep 0.25; \
	  done; \
	  curl -sf "http://localhost:$$port/healthz" >/dev/null \
	    || { echo "-> Server never came up. Log:"; cat /tmp/code_scout_e2e.log; exit 1; }; \
	  CS_E2E_BASE="http://localhost:$$port" CS_DB_NAME=$$db node --test --test-concurrency=1 e2e/

## Run the Flutter SDK against a real server (needs the SDK checked out beside this)
##
## The browser tests seed through the ingest endpoint with a hand-written
## archive, which proves the server reads that shape but not that the SDK still
## sends it. This runs the actual SDK — its compressor, its uploader, its
## session records — and reads the rows back out of the dashboard.
##
## Its own port and database so it can run beside `make test-e2e`. Override the
## SDK location with `make test-sdk-e2e sdk_dir=/path/to/code_scout_flutter`.
test-sdk-e2e:
	@ set -a; [ -f .env ] && . ./.env; set +a; \
	  set -e; \
	  [ -d "$(sdk_dir)" ] || { echo "-> No SDK at $(sdk_dir). Pass sdk_dir=..."; exit 1; }; \
	  port=24284; db=code_scout_sdk_e2e; \
	  echo "-> Building the server..."; \
	  go build -o ./bin/code_scout_sdk_e2e . ; \
	  psql -U $(pg_super) -d postgres -q -c "DROP DATABASE IF EXISTS $$db;"; \
	  psql -U $(pg_super) -d postgres -q -c "CREATE DATABASE $$db OWNER $$CS_DB_USER;"; \
	  CS_DB_NAME=$$db CS_PORT=$$port ./bin/code_scout_sdk_e2e > /tmp/code_scout_sdk_e2e.log 2>&1 & \
	  server=$$!; \
	  trap 'kill $$server 2>/dev/null; psql -U $(pg_super) -d postgres -q -c "DROP DATABASE IF EXISTS $$db;" >/dev/null 2>&1' EXIT; \
	  echo "-> Waiting for the server on :$$port..."; \
	  for i in $$(seq 1 40); do \
	    curl -sf "http://localhost:$$port/healthz" >/dev/null && break; \
	    sleep 0.25; \
	  done; \
	  curl -sf "http://localhost:$$port/healthz" >/dev/null \
	    || { echo "-> Server never came up. Log:"; cat /tmp/code_scout_sdk_e2e.log; exit 1; }; \
	  echo "-> Running the SDK against it..."; \
	  cd "$(sdk_dir)" && CS_E2E_BASE="http://localhost:$$port" flutter test test/e2e/

## Regenerate the README screenshots into .github/assets
##
## Same throwaway server and database as the browser tests, seeded through the
## real ingest endpoint. Rerun it whenever a screen changes; a screenshot nobody
## can regenerate is one that quietly goes out of date.
screenshots:
	@ set -a; [ -f .env ] && . ./.env; set +a; \
	  set -e; \
	  port=24285; db=code_scout_shots; \
	  go build -o ./bin/code_scout_shots . ; \
	  psql -U $(pg_super) -d postgres -q -c "DROP DATABASE IF EXISTS $$db;"; \
	  psql -U $(pg_super) -d postgres -q -c "CREATE DATABASE $$db OWNER $$CS_DB_USER;"; \
	  CS_DB_NAME=$$db CS_PORT=$$port ./bin/code_scout_shots > /tmp/code_scout_shots.log 2>&1 & \
	  server=$$!; \
	  trap 'kill $$server 2>/dev/null; psql -U $(pg_super) -d postgres -q -c "DROP DATABASE IF EXISTS $$db;" >/dev/null 2>&1' EXIT; \
	  for i in $$(seq 1 40); do curl -sf "http://localhost:$$port/healthz" >/dev/null && break; sleep 0.25; done; \
	  CS_E2E_BASE="http://localhost:$$port" node e2e/screenshots.js

## Same browser tests, in a visible window you can watch
##
## Slowed to 300ms per action so it is followable; CS_E2E_SLOWMO=0 for full
## speed, or any millisecond value.
test-e2e-headed:
	@ CS_E2E_HEADED=1 $(MAKE) --no-print-directory test-e2e

## First-time local setup: creates .env and the local database
dev-setup: env db
	@ echo ""
	@ echo "-> Ready. Start the server with: ${YELLOW}make dev${RESET}"

## Create .env from .env.example if it does not exist
env:
	@ if [ -f .env ]; then \
		echo "-> .env already exists, leaving it alone."; \
	else \
		cp .env.example .env; \
		echo "-> Created .env from .env.example."; \
	fi

## Create the local database and role (reads .env)
db:
	@ set -a; [ -f .env ] && . ./.env; set +a; \
	  echo "-> Creating database '$$CS_DB_NAME' and role '$$CS_DB_USER'..."; \
	  psql -U $(pg_super) -d postgres -q -c \
	    "DO \$$\$$ BEGIN CREATE ROLE $$CS_DB_USER LOGIN PASSWORD '$$CS_DB_PASSWORD'; \
	     EXCEPTION WHEN duplicate_object THEN \
	       ALTER ROLE $$CS_DB_USER LOGIN PASSWORD '$$CS_DB_PASSWORD'; END \$$\$$;" && \
	  ( psql -U $(pg_super) -d postgres -tAc \
	      "SELECT 1 FROM pg_database WHERE datname='$$CS_DB_NAME'" | grep -q 1 \
	    || psql -U $(pg_super) -d postgres -q -c \
	      "CREATE DATABASE $$CS_DB_NAME OWNER $$CS_DB_USER;" ) && \
	  echo "-> Done. ✓"

# The schema is not migrated, it is recreated: the server AutoMigrates on
# startup, so the next `make dev` rebuilds every table from the current models.
# That is the whole reason this exists — there is no deployed instance to
# migrate, so a model change is a rebuild rather than a migration, and this is
# the command that makes that cheap.
#
# Connections are terminated first. DROP DATABASE fails outright while anything
# is attached, and a forgotten `air` in another terminal is enough to do it.
## Wipe a database and rebuild it empty (host=my-server for a remote; force=1 skips the prompt)
db-reset:
ifdef host
	@ echo "-> Reading the database name from $(host)..."
	@ db=$$(ssh $(host) "sudo grep '^db_name' /etc/code-scout.conf | cut -d'\"' -f2"); \
	  owner=$$(ssh $(host) "sudo grep '^db_user' /etc/code-scout.conf | cut -d'\"' -f2"); \
	  if [ -z "$$db" ] || [ -z "$$owner" ]; then \
	    echo "-> Could not read db_name/db_user from /etc/code-scout.conf on $(host)."; exit 1; \
	  fi; \
	  if [ "$(force)" != "1" ]; then \
	    printf "${RED}This deletes every row in '$$db' on $(host)${RESET} — projects, logs, accounts.\n"; \
	    printf "Type the host to confirm: "; \
	    read answer; \
	    if [ "$$answer" != "$(host)" ]; then echo "-> Left alone."; exit 1; fi; \
	  fi; \
	  echo "-> Stopping the service..."; \
	  ssh $(host) "sudo systemctl stop code_scout" && \
	  echo "-> Dropping '$$db'..."; \
	  ssh $(host) "sudo -u postgres psql -q -c \"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$$db' AND pid <> pg_backend_pid();\" > /dev/null && \
	    sudo -u postgres psql -q -c \"DROP DATABASE IF EXISTS $$db;\" && \
	    sudo -u postgres psql -q -c \"CREATE DATABASE $$db OWNER $$owner;\"" && \
	  echo "-> Starting the service..." && \
	  ssh $(host) "sudo systemctl start code_scout" && \
	  echo "-> Empty database ready on $(host). ✓" && \
	  echo "   The service has rebuilt the tables. The first account you register is the super admin."
else
	@ set -a; [ -f .env ] && . ./.env; set +a; \
	  if [ "$(force)" != "1" ]; then \
	    printf "${RED}This deletes every row in '$$CS_DB_NAME'${RESET} — projects, logs, accounts.\n"; \
	    printf "Type the database name to confirm: "; \
	    read answer; \
	    if [ "$$answer" != "$$CS_DB_NAME" ]; then echo "-> Left alone."; exit 1; fi; \
	  fi; \
	  echo "-> Dropping '$$CS_DB_NAME'..."; \
	  psql -U $(pg_super) -d postgres -q -c \
	    "SELECT pg_terminate_backend(pid) FROM pg_stat_activity \
	     WHERE datname = '$$CS_DB_NAME' AND pid <> pg_backend_pid();" >/dev/null && \
	  psql -U $(pg_super) -d postgres -q -c "DROP DATABASE IF EXISTS $$CS_DB_NAME;" && \
	  psql -U $(pg_super) -d postgres -q -c \
	    "CREATE DATABASE $$CS_DB_NAME OWNER $$CS_DB_USER;" && \
	  echo "-> Empty database ready. ✓" && \
	  echo "   ${YELLOW}make dev${RESET} recreates the tables and the first account you register is the super admin."
endif

## Run locally with hot reload (templ watch + air)
dev: check-env
	@ echo "-> Starting Code Scout on http://$(CS_HOST):$(CS_PORT)"
	@ make templ & sleep 1
	@ air

# Fails early with a useful message rather than letting the server exit on
# missing configuration.
check-env:
	@ if [ -z "$(CS_DB_HOST)" ]; then \
		echo "${RED}No database configuration found.${RESET}"; \
		echo "Run ${YELLOW}make dev-setup${RESET} first, or create .env from .env.example."; \
		exit 1; \
	fi
	@ command -v air >/dev/null 2>&1 || { \
		echo "${RED}air is not installed.${RESET}"; \
		echo "Install it with: ${YELLOW}go install github.com/air-verse/air@latest${RESET}"; \
		exit 1; }
	@ command -v templ >/dev/null 2>&1 || { \
		echo "${RED}templ is not installed.${RESET}"; \
		echo "Install it with: ${YELLOW}go install github.com/a-h/templ/cmd/templ@latest${RESET}"; \
		exit 1; }

## Alias for dev
run: dev


## Deploy to a remote host over SSH (e.g. make deploy host=my-server)
deploy:
ifndef host
	$(error host is required – use an SSH config alias or user@ip)
endif
	@ echo "-> Building CSS..."
	@ npm run build
	@ echo "-> Building binary..."
	@ GOOS=linux GOARCH=amd64 go build -o ./bin/${binary_name} -ldflags="$(ldflags)" .
	@ echo "-> Build done."
	@ echo "-> Deploying to $(host)..."
	@ scp ./bin/${binary_name} $(host):~/
	@ ssh $(host) "sudo systemctl stop code_scout 2>/dev/null; sudo mv /usr/local/bin/code_scout /usr/local/bin/code_scout_old 2>/dev/null; sudo mv ~/code_scout /usr/local/bin/ && sudo systemctl start code_scout"
	@ echo "-> Done. ✓"

## Prepare a remote host: database + systemd (first time only: make deploy-setup host=my-server)
deploy-setup:
ifndef host
	$(error host is required – use an SSH config alias or user@ip)
endif
	@ echo "-> Setting up code_scout on $(host)..."
	@ CS_DB_PW=$$(openssl rand -base64 18) && \
	ssh $(host) "command -v psql >/dev/null || { sudo apt-get update -qq && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq postgresql; }" && \
	ssh $(host) "sudo -u postgres psql -c \"CREATE ROLE code_scout LOGIN PASSWORD '$$CS_DB_PW';\" ; \
		sudo -u postgres psql -c \"CREATE DATABASE code_scout OWNER code_scout;\"" && \
	printf 'port = 24275\n\n# Database\ndb_user = \"code_scout\"\ndb_password = \"'"$$CS_DB_PW"'\"\ndb_name = \"code_scout\"\ndb_host = \"localhost\"\ndb_port = 5432\n' \
		| ssh $(host) "sudo tee /etc/code-scout.conf > /dev/null && sudo chmod 600 /etc/code-scout.conf" && \
	printf '[Unit]\nDescription=Code Scout Server\nAfter=network.target postgresql.service\n\n[Service]\nType=simple\nExecStart=/usr/local/bin/code_scout\nRestart=on-failure\nRestartSec=5\nStandardOutput=journal\nStandardError=journal\n\n[Install]\nWantedBy=multi-user.target\n' \
		| ssh $(host) "sudo tee /etc/systemd/system/code_scout.service > /dev/null && sudo systemctl daemon-reload && sudo systemctl enable code_scout"
	@ echo "-> Setup complete. Run 'make deploy host=$(host)' to deploy."

## Rebuild the Tailwind CSS bundle
tailwind:
	@ npx tailwindcss -o ./view/static/css/tailwind.css --minify

notify-templ-proxy:
	@ templ generate --notify-proxy --proxyport=$(TEMPL_PROXY_PORT)

templ:
	@TEMPL_EXPERIMENT=rawgo templ generate --watch --proxy=http://localhost:$(APP_PORT) --proxyport=$(TEMPL_PROXY_PORT) --open-browser=false --proxybind="0.0.0.0"

