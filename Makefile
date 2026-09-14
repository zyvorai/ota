GO ?= go
VERSION := 0.1.0

.PHONY: build test race vet check demo dist qualify hil clean \
	deploy deploy-remote deploy-remote-quick deploy-remote-preflight \
	deploy-remote-verify deploy-remote-uninstall deploy-remote-fleet
build:
	mkdir -p bin
	$(GO) build -buildvcs=false -trimpath -o bin/zyvor-ota ./cmd/zyvor-ota
	$(GO) build -buildvcs=false -trimpath -o bin/zyvor-otad ./cmd/zyvor-otad
	$(GO) build -buildvcs=false -trimpath -o bin/zyvor-fleet-ref ./cmd/zyvor-fleet-ref
test:
	$(GO) test ./...
race:
	$(GO) test -race ./...
vet:
	$(GO) vet ./...
check: test race vet build
demo: build
	python3 scripts/e2e.py
qualify: build
	python3 scripts/qualify-matrix.py
hil:
	OTA_HIL_STRICT=$${OTA_HIL_STRICT:-1} ./scripts/hil/run-rauc-powerloss-hil.sh
dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-ota-linux-amd64 ./cmd/zyvor-ota
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-otad-linux-amd64 ./cmd/zyvor-otad
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-fleet-ref-linux-amd64 ./cmd/zyvor-fleet-ref
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-ota-linux-arm64 ./cmd/zyvor-ota
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-otad-linux-arm64 ./cmd/zyvor-otad
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-fleet-ref-linux-arm64 ./cmd/zyvor-fleet-ref
	cp LICENSE NOTICE THIRD_PARTY_NOTICES.md dist/
	cp vendor/github.com/godbus/dbus/v5/LICENSE dist/GODBUS-LICENSE
	cp "$$($(GO) env GOROOT)/LICENSE" dist/GO-LICENSE
clean:
	rm -rf bin dist coverage.out

# Remote smoke-deploy (deploy/README.md). NOT real device deployment — see
# docs/DEVICE-INTEGRATION.md for board image integration.
deploy: deploy-remote ## Alias for deploy-remote

deploy-remote: ## Full remote smoke-deploy: make deploy-remote H=ip U=root
	bash scripts/deploy-remote.sh $(H) $(or $(U),root) $(PASS) --key $(ARGS)

deploy-remote-quick: ## Quick remote deploy: make deploy-remote-quick H=ip U=root
	bash scripts/deploy-remote.sh $(H) $(or $(U),root) $(PASS) --key --quick $(ARGS)

deploy-remote-preflight: ## SSH preflight only: make deploy-remote-preflight H=ip
	bash scripts/deploy-remote.sh $(H) $(or $(U),root) --key --preflight-only

deploy-remote-verify: ## Remote selftest only: make deploy-remote-verify H=ip
	bash scripts/deploy-remote.sh $(H) $(or $(U),root) --key --verify-only

deploy-remote-uninstall: ## Remove the demo service from host: make deploy-remote-uninstall H=ip
	bash scripts/deploy-remote.sh $(H) $(or $(U),root) --key --uninstall

deploy-remote-fleet: ## Fleet deploy: make deploy-remote-fleet FILE=hosts.txt
	bash scripts/deploy-remote.sh --fleet $(FILE)
