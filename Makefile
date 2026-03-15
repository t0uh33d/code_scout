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

.PHONY: build-local build templ notify-templ-proxy run

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

## Run unit & integration tests
test:
	@ echo "-> Running tests..."
	@ go test ./...  -tags=integration
	@ echo "-> Done.  ✓"

## Run the project
run:
	@ echo "-> Running project..."
	@ make templ & sleep 1
	@ air


## Build, upload, and restart on a remote host (e.g. make up host=my-server)
up:
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

## Setup systemd service on remote host (first-time only: make setup host=my-server)
setup:
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
	@ echo "-> Setup complete. Run 'make up host=$(host)' to deploy."

tailwind:
	@ npx tailwindcss -i ./assets/css/input.css -o ./public/css/output.css --watch

notify-templ-proxy:
	@ templ generate --notify-proxy --proxyport=$(TEMPL_PROXY_PORT)

templ:
	@TEMPL_EXPERIMENT=rawgo templ generate --watch --proxy=http://localhost:$(APP_PORT) --proxyport=$(TEMPL_PROXY_PORT) --open-browser=false --proxybind="0.0.0.0"

