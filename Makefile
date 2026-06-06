.PHONY: build build-linux build-windows build-all build-bot build-bot-linux build-bot-windows tidy init

VPNCTL=./cmd/vpnctl
TGBOT=./cmd/tgbot
DIST=dist

build:
	CGO_ENABLED=0 go build -o vpnctl $(VPNCTL)
	CGO_ENABLED=0 go build -o tgbot $(TGBOT)

build-linux:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(DIST)/vpnctl-linux-amd64 $(VPNCTL)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(DIST)/tgbot-linux-amd64 $(TGBOT)

build-windows:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(DIST)/vpnctl-windows-amd64.exe $(VPNCTL)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(DIST)/tgbot-windows-amd64.exe $(TGBOT)

build-bot-linux:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(DIST)/tgbot-linux-amd64 $(TGBOT)

build-bot-windows:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(DIST)/tgbot-windows-amd64.exe $(TGBOT)

build-all: build-linux build-windows
	@echo "built $(DIST)/vpnctl-* and $(DIST)/tgbot-*"

init tidy:
	go mod tidy
