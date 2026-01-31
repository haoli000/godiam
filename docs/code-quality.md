# Code Quality

Guidelines and tooling for maintaining code quality in godiam.

## Quick Reference

```bash
make check    # Run all checks: fmt → tidy → vet → lint → test
make fmt      # Format code (gofmt -s)
make tidy     # Verify go.mod/go.sum are tidy
make vet      # Static analysis (go vet)
make lint     # Linter suite (golangci-lint)
make test     # Tests with race detector
```

Always run `make check` before committing. All checks must pass with zero issues.

## Toolchain

| Tool | Version | Purpose |
|------|---------|---------|
| `gofmt -s` | (bundled with Go) | Code formatting + simplification |
| `go vet` | (bundled with Go) | Built-in static analysis |
| `golangci-lint` | v2.x | Meta-linter running 16 linters in parallel |
| `go test -race` | (bundled with Go) | Tests with race condition detection |

## Linter Configuration

Configuration lives in `.golangci.yml` (golangci-lint v2 format). The following linters are enabled:

### Correctness

| Linter | What it catches |
|--------|-----------------|
| **errcheck** | Unchecked error return values |
| **govet** | Suspicious constructs (printf format strings, struct tags, etc.) |
| **staticcheck** | Comprehensive static analysis (nil derefs, unused results, deprecated APIs) |
| **unused** | Unused constants, variables, functions, types |
| **ineffassign** | Assignments to variables that are never read |
| **bodyclose** | HTTP response bodies not closed |
| **noctx** | HTTP requests without `context.Context` |
| **copyloopvar** | Loop variable capture bugs |

### Error Handling

| Linter | What it catches |
|--------|-----------------|
| **errorlint** | Incorrect error comparisons (`==` instead of `errors.Is`) and type assertions |
| **errname** | Sentinel errors not prefixed with `Err`, error types not suffixed with `Error` |
| **exhaustive** | Non-exhaustive `switch` statements on enum types |

### Security

| Linter | What it catches |
|--------|-----------------|
| **gosec** | Security issues: missing `ReadHeaderTimeout` (Slowloris), weak crypto, file perms, hardcoded creds |

**Excluded rules:**
- `G101` — hardcoded credential detection (too many false positives on test data and Diameter AVP labels)

### Style & Consistency

| Linter | What it catches |
|--------|-----------------|
| **revive** | Go style rules (exported doc comments, naming, imports, error returns) |
| **gocritic** | Diagnostic and performance suggestions |
| **misspell** | Typos in comments and strings |
| **unparam** | Unused function parameters |

## Key Rules & Patterns

### All exported identifiers must have doc comments

```go
// Good
// Router manages message routing and dispatch.
type Router struct { ... }

// Bad — will fail revive/exported
type Router struct { ... }
```

### Use `errors.Is` / `errors.As`, not `==`

```go
// Good
if err != nil && !errors.Is(err, net.ErrClosed) { ... }

// Bad — fails errorlint
if err != nil && err != net.ErrClosed { ... }
```

### Always set `ReadHeaderTimeout` on `http.Server`

```go
// Good — prevents Slowloris attacks (gosec G112)
&http.Server{
    Addr:              ":9090",
    Handler:           mux,
    ReadHeaderTimeout: 10 * time.Second,
}

// Bad — missing timeout
&http.Server{Addr: ":9090", Handler: mux}
```

### Avoid type name stuttering

```go
// Good — called as peer.Config
type Config struct { ... }

// Bad — called as peer.PeerConfig (stutters)
type PeerConfig struct { ... }
```

### File permissions in tests

```go
// Good — restrictive permissions (gosec G306)
os.WriteFile(path, data, 0600)

// Bad — world-readable
os.WriteFile(path, data, 0644)
```

## Excluded Directories

The linter skips `helm/`, `certs/`, `examples/`, `tests/`, and `diameterbench/` (non-Go or integration-only files).

## Adding a New Linter

1. Check availability: `golangci-lint help linters`
2. Add to `.golangci.yml` under `linters.enable`
3. Run `make lint` to see new issues
4. Fix issues or add settings under `linters-settings`
5. Document the linter in this file

## CI Integration

Run `make check` in CI pipelines. It exits non-zero on any failure:

```yaml
# Example GitHub Actions step
- name: Code quality
  run: make check
```
