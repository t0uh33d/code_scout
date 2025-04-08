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


## Build and upload the HAI API to a remote host specified by target (e.g. make up host=1.2.3.4 user=ubuntu)
up:
	@ echo "-> Building project..."
	@ npm run build
	@ GOOS=linux GOARCH=amd64 go build -o ./bin/${binary_name} -ldflags="-X 'main.BuildTime=$$(date)' -X 'main.BranchName=$$(git branch --show-current)' -X 'main.CommitHash=$$(git rev-parse HEAD)' -X 'main.DirtyFiles=$$(git status --porcelain)'" main.go
	@ echo "-> Build done."

	@ echo "-> Uploading binary to" $(host)
	@ scp ./bin/${binary_name} $(user)@$(host):~/
	@ echo "-> Upload binary done."

	@ echo "-> Restarting service..."
	@ ssh $(user)@$(host) "sudo systemctl stop ${service_name}"
	@ ssh $(user)@$(host) "sudo mv /usr/local/bin/${binary_name} /usr/local/bin/${binary_name}_old"
	@ ssh $(user)@$(host) "sudo mv ${binary_name} /usr/local/bin/"
	@ ssh $(user)@$(host) "sudo systemctl start ${service_name}"
	@ echo "-> Restarting service done."
	@ echo "-> Done. ✓"

tailwind:
	@ npx tailwindcss -i ./assets/css/input.css -o ./public/css/output.css --watch

notify-templ-proxy:
	@ templ generate --notify-proxy --proxyport=$(TEMPL_PROXY_PORT)

templ:
	@TEMPL_EXPERIMENT=rawgo templ generate --watch --proxy=http://localhost:$(APP_PORT) --proxyport=$(TEMPL_PROXY_PORT) --open-browser=false --proxybind="0.0.0.0"

