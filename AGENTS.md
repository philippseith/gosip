# AGENTS.md

Go implementation of S/IP (Sercos Internet Protocol). Single module, no Makefile, no CI, no codegen.

## Commands

```sh
go build ./...
go vet ./...
golangci-lint run ./...   # skips *_test.go files (run: tests: false in .golangci.yaml)
gofmt -w .
go mod tidy
```

Run tests — **read the test prerequisites below first**:

```sh
go test ./...             # both packages; sip/ requires a live device
go test ./sip_test/...    # safe, fully in-memory
go test ./sip -run <Name> # hardware tests; needs test_config.json
```

## Test packages — two separate directories, different requirements

### `sip_test/` — safe to run anytime

Uses an in-memory `net.Pipe()`-based listener and a stub client. No real device needed.
One test (`TestMuxServe`) binds `127.0.0.1:8086` — fails if that port is busy.

### `sip/` — requires a real Sercos device

`TestMain` calls `viper.SetConfigFile("testdata/test_config.json")` and **panics** if the
file is missing. The file is gitignored and must be created manually:

```json
{
    "interfaceName": "<network-interface-name>",
    "serverAddress": "<ip>:<port>",
    "identifyNode": [0, 0, 0, 0, 0, 0]
}
```

Default S/IP port is **35021**. All `TestConnect`, `TestPing`, `TestReadEverything`,
`TestBrowse`, `TestIdentify`, `TestStress`, etc. make real TCP/UDP calls to this device.

## Disabled-test convention

Tests prefixed with `_` (e.g., `_TestUDP`, `_TestBroadcast`, `_TestP1354`) are intentionally
invisible to the test runner. This is the project's way of parking hardware-dependent or
incomplete tests. **Do not rename them** to re-enable — add `t.Skip()` instead if you need
a proper skip.

## Pre-built test binaries

`sip.test` and `sip_test.test` in the repo root are committed pre-built binaries for direct
execution against hardware. `.gitignore` lists `*.test` but these are intentionally kept.
Do not delete them.

## Code conventions

- **Error wrapping:** use `errorx.EnsureStackTrace(err)` / `errorx.EnhanceStackTrace(err, ...)`,
  not `fmt.Errorf`. Standard `%w` is still used inside `errorx` calls for `errors.Is`/`errors.As`
  compatibility.
- **Logging:** disabled by default (discarded). Call `sip.EnableLogging(true)` to route to stderr.
- **Wire format:** little-endian throughout (`encoding/binary` + `binary.LittleEndian`).
- **Nil-interface guard:** `Dial()` wraps the private `dial()` specifically to avoid returning a
  non-nil interface holding a nil pointer — preserve this pattern in any new connection constructors.
- **`nolint` suppressions** are inline (`//nolint:gosec`), not in `.golangci.yaml`.
- **`dev_test.go`** uses `package sip` (internal); all other test files in `sip/` use
  `package sip_test` (external). Keep this separation.

## Key dependencies

| Package | Role |
|---|---|
| `github.com/joomcode/errorx` | Stack-trace-enriched errors |
| `github.com/cenkalti/backoff/v4` | Exponential backoff in `Client` reconnect |
| `github.com/spf13/viper` | Config reading in `sip/` `TestMain` only |
| `github.com/stretchr/testify` | Test assertions |
