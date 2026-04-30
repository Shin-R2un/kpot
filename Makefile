.PHONY: build test install clean help check vet fmt fmt-check bench \
        release-patch release-minor release-major \
        install-hooks uninstall-hooks

BIN := kpot
PKG := ./cmd/kpot

# --- Build ---
# `make` (no args) builds. `make help` lists everything available.

build:
	go build -o $(BIN) $(PKG)

install:
	go install $(PKG)

clean:
	rm -f $(BIN)

# --- Quality gates ---
# `make check` matches the CI matrix step exactly so green here = green there.

test:
	go test ./... -count=1 -timeout=120s

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l . | grep -v '^vendor/' || true); \
		if [ -n "$$unformatted" ]; then \
			echo "gofmt found unformatted files:"; \
			echo "$$unformatted"; \
			echo ""; \
			echo "Run 'make fmt' to auto-fix."; \
			exit 1; \
		fi

check: vet fmt-check test

# `make bench` runs every Benchmark in the tree without the regular
# tests (-run=^$). benchtime is short so it fits a normal dev cycle;
# bump it explicitly for release-time baseline captures, e.g.:
#   go test ./... -run=^$ -bench=. -benchtime=3s -benchmem
# Baseline numbers (and the rationale for them) live in docs/perf.md.
bench:
	go test ./... -run=^$$ -bench=. -benchmem -count=1

# --- Release automation ---
# `make release-patch`  v0.6.0 -> v0.6.1
# `make release-minor`  v0.6.0 -> v0.7.0
# `make release-major`  v0.6.0 -> v1.0.0
# Append YES=1 to skip the confirmation prompt:
#   make release-patch YES=1

release-patch:
	@bash scripts/release.sh patch $(if $(YES),--yes,)

release-minor:
	@bash scripts/release.sh minor $(if $(YES),--yes,)

release-major:
	@bash scripts/release.sh major $(if $(YES),--yes,)

# --- Git hooks ---
# `make install-hooks` installs .git/hooks/pre-push that runs `make check`
# before every push. Skip in an emergency with `git push --no-verify`.
# `--git-path hooks` resolves correctly for both regular repos and worktrees.

install-hooks:
	@hookdir=$$(git rev-parse --git-path hooks); \
		install -m 0755 scripts/pre-push $$hookdir/pre-push; \
		echo "✓ pre-push hook installed at $$hookdir/pre-push"; \
		echo "  Skip in an emergency with: git push --no-verify"

uninstall-hooks:
	@hookdir=$$(git rev-parse --git-path hooks); \
		rm -f $$hookdir/pre-push; \
		echo "✓ pre-push hook removed from $$hookdir/pre-push"

# --- Help ---

help:
	@echo "kpot make targets:"
	@echo ""
	@echo "  Build:"
	@echo "    build           go build -> ./kpot  (default if you just run 'make')"
	@echo "    install         go install ./cmd/kpot -> \$$GOPATH/bin"
	@echo "    clean           rm -f ./kpot"
	@echo ""
	@echo "  Quality (mirrors CI):"
	@echo "    test            go test ./... -count=1 -timeout=120s"
	@echo "    vet             go vet ./..."
	@echo "    fmt             gofmt -w .   (auto-format)"
	@echo "    fmt-check       gofmt -l .   (check only, no rewrite)"
	@echo "    check           vet + fmt-check + test"
	@echo "    bench           run all benchmarks (no regular tests)"
	@echo ""
	@echo "  Release (auto-bumps version, tags, pushes; triggers GitHub Actions):"
	@echo "    release-patch   v0.6.0 -> v0.6.1"
	@echo "    release-minor   v0.6.0 -> v0.7.0"
	@echo "    release-major   v0.6.0 -> v1.0.0"
	@echo "    Append YES=1 to skip the confirmation prompt."
	@echo ""
	@echo "  Hooks:"
	@echo "    install-hooks   install .git/hooks/pre-push (runs 'make check')"
	@echo "    uninstall-hooks remove .git/hooks/pre-push"
