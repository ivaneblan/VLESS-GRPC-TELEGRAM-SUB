.PHONY: build build-linux build-windows build-all build-bot build-bot-linux build-bot-windows tidy init

VPNCTL=./cmd/vpnctl
TGBOT=./cmd/tgbot
DIST=dist

ifeq ($(OS),Windows_NT)
MKDIR = if not exist $(DIST) mkdir $(DIST)
GOENV = set CGO_ENABLED=0&&
GOENV_LINUX = set CGO_ENABLED=0&& set GOOS=linux&& set GOARCH=amd64&&
GOENV_WINDOWS = set CGO_ENABLED=0&& set GOOS=windows&& set GOARCH=amd64&&
GO ?= C:/PROGRA~1/Go/bin/go.exe
GO_CMD = $(GO)
VPNCTL_BIN = vpnctl.exe
TGBOT_BIN = tgbot.exe
else
MKDIR = mkdir -p $(DIST)
GOENV = CGO_ENABLED=0
GOENV_LINUX = CGO_ENABLED=0 GOOS=linux GOARCH=amd64
GOENV_WINDOWS = CGO_ENABLED=0 GOOS=windows GOARCH=amd64
GO := go
GO_CMD = $(GO)
VPNCTL_BIN = vpnctl
TGBOT_BIN = tgbot
endif

build:
	$(GOENV) $(GO_CMD) build -o $(VPNCTL_BIN) $(VPNCTL)
	$(GOENV) $(GO_CMD) build -o $(TGBOT_BIN) $(TGBOT)

build-linux:
	@$(MKDIR)
	$(GOENV_LINUX) $(GO_CMD) build -o $(DIST)/vpnctl-linux-amd64 $(VPNCTL)
	$(GOENV_LINUX) $(GO_CMD) build -o $(DIST)/tgbot-linux-amd64 $(TGBOT)

build-windows:
	@$(MKDIR)
	$(GOENV_WINDOWS) $(GO_CMD) build -o $(DIST)/vpnctl-windows-amd64.exe $(VPNCTL)
	$(GOENV_WINDOWS) $(GO_CMD) build -o $(DIST)/tgbot-windows-amd64.exe $(TGBOT)

build-bot-linux:
	@$(MKDIR)
	$(GOENV_LINUX) $(GO_CMD) build -o $(DIST)/tgbot-linux-amd64 $(TGBOT)

build-bot-windows:
	@$(MKDIR)
	$(GOENV_WINDOWS) $(GO_CMD) build -o $(DIST)/tgbot-windows-amd64.exe $(TGBOT)

build-all: build-linux build-windows build
	@echo "built $(DIST)/vpnctl-* and $(DIST)/tgbot-* + local vpnctl/tgbot"

init tidy:
	$(GO_CMD) mod tidy
