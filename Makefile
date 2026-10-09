.PHONY: build test vet cross link fuzz soak snapshot vulncheck
build:
	sh scripts/build.sh
test:
	go test -race ./...
vet:
	go vet ./...
# cross checks Linux and Windows from macOS: builds, vet and compiled (not run)
# Windows test binaries. A real Windows run needs CI or a Windows machine.
cross:
	GOOS=linux GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=amd64 go vet ./...
	mkdir -p "$${TMPDIR:-/tmp}/herdr-dots-wintest"
	GOOS=windows GOARCH=amd64 go test -c -o "$${TMPDIR:-/tmp}/herdr-dots-wintest/" ./...
link: build
	herdr --session dots-test plugin link .
# fuzz runs every fuzz target for FUZZTIME each (default 30s).
fuzz:
	sh scripts/fuzz.sh $(or $(FUZZTIME),30s)
# soak runs the soak test for SOAK_DURATION (default 15m); 8h for a release.
soak:
	SOAK_DURATION=$(or $(SOAK_DURATION),15m) go test -tags soak -run TestSoak -timeout 10h -v ./internal/engine/
# snapshot builds the six release archives and dist/SHA256SUMS locally. It
# never publishes: GoReleaser in snapshot mode when installed, else plain go build.
snapshot:
	@if command -v goreleaser >/dev/null 2>&1; then \
		HERDR_DOTS_VERSION=$$(tr -d ' \r\n' < internal/version/VERSION) \
		ASSET_COMMIT=$$(git rev-parse --short=12 HEAD) ASSET_TIME=$$(git log -1 --format=%ct HEAD) \
		goreleaser release --snapshot --clean --skip=publish,sign; \
	else \
		sh scripts/release-local.sh; \
	fi
vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
