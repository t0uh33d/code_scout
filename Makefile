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

# Local settings live in .env (gitignored), so development needs no
# /etc/code-scout.conf. Anything already set in the environment wins.
-include .env
export

# Root credentials for `make db`, override if your local MySQL differs:
#   make db mysql_root=root mysql_root_pw=secret
mysql_root ?= root
mysql_root_pw ?=

.PHONY: build build-local templ notify-templ-proxy run dev db env test

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
	@ GOOS=linux GOARCH=amd64 go build -o ./bin/${binary_name} -ldflags="-X 'main.BuildTime=$$(date)' -X 'main.BranchName=$$(git branch --show-current)' -X 'main.CommitHash=$$(git rev-parse HEAD)' -X 'main.DirtyFiles=$$(git status --porcelain)'" main.go
	@ echo "-> Done. ✓"

## Run the tests
test:
	@ echo "-> Running tests..."
	@ go test ./...
	@ echo "-> Done.  ✓"

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

## Create the local database and user (reads .env)
db:
	@ echo "-> Creating database '$(CS_MYSQL_DATABASE)' and user '$(CS_MYSQL_USER)'..."
	@ mysql -u $(mysql_root) $(if $(mysql_root_pw),-p$(mysql_root_pw),) -e "\
		CREATE DATABASE IF NOT EXISTS \`$(CS_MYSQL_DATABASE)\` CHARACTER SET utf8mb4; \
		CREATE USER IF NOT EXISTS '$(CS_MYSQL_USER)'@'localhost' IDENTIFIED BY '$(CS_MYSQL_PASSWORD)'; \
		CREATE USER IF NOT EXISTS '$(CS_MYSQL_USER)'@'127.0.0.1' IDENTIFIED BY '$(CS_MYSQL_PASSWORD)'; \
		GRANT ALL PRIVILEGES ON \`$(CS_MYSQL_DATABASE)\`.* TO '$(CS_MYSQL_USER)'@'localhost'; \
		GRANT ALL PRIVILEGES ON \`$(CS_MYSQL_DATABASE)\`.* TO '$(CS_MYSQL_USER)'@'127.0.0.1'; \
		FLUSH PRIVILEGES;"
	@ echo "-> Done. ✓"

## Run locally with hot reload (templ watch + air)
dev: check-env
	@ echo "-> Starting Code Scout on http://$(CS_HOST):$(CS_PORT)"
	@ make templ & sleep 1
	@ air

# Fails early with a useful message rather than letting the server exit on
# missing configuration.
check-env:
	@ if [ -z "$(CS_MYSQL_HOST)" ]; then \
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
	@ GOOS=linux GOARCH=amd64 go build -o ./bin/${binary_name} -ldflags="-X 'main.BuildTime=$$(date)' -X 'main.BranchName=$$(git branch --show-current)' -X 'main.CommitHash=$$(git rev-parse HEAD)' -X 'main.DirtyFiles=$$(git status --porcelain)'" main.go
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
	ssh $(host) "sudo mysql -e \"\
		CREATE DATABASE IF NOT EXISTS code_scout_db; \
		CREATE USER IF NOT EXISTS 'code_scout'@'localhost' IDENTIFIED BY '$$CS_DB_PW'; \
		GRANT ALL PRIVILEGES ON code_scout_db.* TO 'code_scout'@'localhost'; \
		FLUSH PRIVILEGES;\"" && \
	printf 'port = 24275\n\n# MySQL Database Configuration\nmysql_user = \"code_scout\"\nmysql_password = \"'"$$CS_DB_PW"'\"\nmysql_database = \"code_scout_db\"\nmysql_host = \"localhost\"\nmysql_port = 3306\n' \
		| ssh $(host) "sudo tee /etc/code-scout.conf > /dev/null && sudo chmod 600 /etc/code-scout.conf" && \
	printf '[Unit]\nDescription=Code Scout Server\nAfter=network.target mariadb.service\n\n[Service]\nType=simple\nExecStart=/usr/local/bin/code_scout\nRestart=on-failure\nRestartSec=5\nStandardOutput=journal\nStandardError=journal\n\n[Install]\nWantedBy=multi-user.target\n' \
		| ssh $(host) "sudo tee /etc/systemd/system/code_scout.service > /dev/null && sudo systemctl daemon-reload && sudo systemctl enable code_scout"
	@ echo "-> Setup complete. Run 'make deploy host=$(host)' to deploy."

## Rebuild the Tailwind CSS bundle
tailwind:
	@ npx tailwindcss -o ./view/static/css/tailwind.css --minify

notify-templ-proxy:
	@ templ generate --notify-proxy --proxyport=$(TEMPL_PROXY_PORT)

templ:
	@TEMPL_EXPERIMENT=rawgo templ generate --watch --proxy=http://localhost:$(APP_PORT) --proxyport=$(TEMPL_PROXY_PORT) --open-browser=false --proxybind="0.0.0.0"

