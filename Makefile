GO ?= go
VERSION := 0.2.0

.PHONY: build test race vet check demo dist qualify hil ci-lab ci-rauc-qemu clean \
	deploy deploy-remote deploy-remote-quick deploy-remote-preflight \
	deploy-remote-verify deploy-remote-uninstall deploy-remote-fleet \
	fmt ci status help measure
build: ## Build primary CLI otactl (+ zyvor-ota alias), daemon, fleet-ref, relay
	mkdir -p bin
	$(GO) build -buildvcs=false -trimpath -o bin/otactl ./cmd/zyvor-ota
	cp -f bin/otactl bin/zyvor-ota
	$(GO) build -buildvcs=false -trimpath -o bin/zyvor-otad ./cmd/zyvor-otad
	$(GO) build -buildvcs=false -trimpath -o bin/zyvor-fleet-ref ./cmd/zyvor-fleet-ref
	$(GO) build -buildvcs=false -trimpath -o bin/zyvor-relay ./cmd/zyvor-relay
test: ## Unit tests
	$(GO) test ./...
race: ## Tests with the race detector
	$(GO) test -race ./...
vet: ## go vet
	$(GO) vet ./...
check: test race vet build ## Tests, race, vet, and build
demo: build
	python3 scripts/e2e.py
ota-demo: build
	python3 scripts/ota-demo up
measure: ## Simulator and localhost relay figures. Not QEMU or Minewing.
	$(GO) run ./scripts/measure
qualify: build
	python3 scripts/qualify-matrix.py
hil:
	OTA_HIL_STRICT=$${OTA_HIL_STRICT:-1} ./scripts/hil/run-rauc-powerloss-hil.sh
ci-lab: build
	python3 scripts/ci/lab-substitute.py
# Soft-skips when QUALIFY_QEMU_IMAGE / QEMU_LAB_DIR disk missing — never Minewing claim.
ci-rauc-qemu:
	bash scripts/ci/rauc-qemu-smoke.sh
dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -o dist/otactl-linux-amd64 ./cmd/zyvor-ota
	cp -f dist/otactl-linux-amd64 dist/zyvor-ota-linux-amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-otad-linux-amd64 ./cmd/zyvor-otad
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-fleet-ref-linux-amd64 ./cmd/zyvor-fleet-ref
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-relay-linux-amd64 ./cmd/zyvor-relay
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -buildvcs=false -trimpath -o dist/otactl-linux-arm64 ./cmd/zyvor-ota
	cp -f dist/otactl-linux-arm64 dist/zyvor-ota-linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-otad-linux-arm64 ./cmd/zyvor-otad
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-fleet-ref-linux-arm64 ./cmd/zyvor-fleet-ref
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -buildvcs=false -trimpath -o dist/zyvor-relay-linux-arm64 ./cmd/zyvor-relay
	cp LICENSE NOTICE THIRD_PARTY_NOTICES.md dist/
	cp vendor/github.com/godbus/dbus/v5/LICENSE dist/GODBUS-LICENSE
	cp "$$($(GO) env GOROOT)/LICENSE" dist/GO-LICENSE
fmt: ## Fail if cmd/ or internal/ need gofmt
	@test -z "$$(gofmt -l cmd internal)" || (echo "Run gofmt on:"; gofmt -l cmd internal; exit 1)

ci: fmt vet race build ## Local gate: gofmt, vet, race tests, build

status: build ## Primary CLI: otactl status (zyvor-ota is the same binary)
	./bin/otactl status

help: ## Show targets (operator CLI is otactl; zyvor-ota is a compat alias)
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | sort | awk -F':.*## ' '{printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

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
