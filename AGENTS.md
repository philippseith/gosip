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

## Graphify

Use the local Graphify CLI as a supplementary structural index for call-flow, dependency, and relationship questions. Check the installed version with `graphify --version`; flags may differ by release.

- From the repository root, use `graphify query "<specific question>" --budget 1500` to find relevant nodes and edges. Narrow call-flow queries with `--context call`; use `--dfs` for deeper traversal.
- Follow up with `graphify explain "<node from query>"` for one concept or `graphify path "<source>" "<target>"` for a relationship path. Use `--graph <path>` when the graph is not at the default `graphify-out/graph.json`.
- If no graph exists and structural context would help, build one with `graphify extract . --code-only`. This skips semantic document/media extraction and needs no model key. Inspect the command summary and use the `graph.json` path it reports. The CLI also supports `--out <dir>` for a separate output root.
- Before extracting, check existing output and repository ignore/tracking rules. Graphify output can contain source-derived context; treat it like the source. `graphify-out/` is not currently ignored by this repository, so prefer a temporary output root for one-off analysis rather than leaving generated files in the checkout.
- Refresh a saved default graph after source changes with `graphify update .`; after a pull or merge, refresh explicitly before querying. For a temporary graph, re-extract to refresh it.
- Treat Graphify output as navigation evidence, not as verified behavior. Distinguish `EXTRACTED` from `INFERRED` edges, inspect cited source lines before relying on a claim, and do not treat a missing node/path as proof that no relationship exists.
- `/graphify .` or `$graphify` is an assistant-chat skill instruction, not a shell command. Use `graphify ...` for CLI operations.

References: [Quickstart](https://docs.graphify.com/quickstart), [querying](https://docs.graphify.com/guides/querying), [CLI reference](https://docs.graphify.com/reference/overview), [incremental updates](https://docs.graphify.com/guides/incremental-updates), [outputs](https://docs.graphify.com/concepts/outputs).

## Key dependencies

| Package | Role |
|---|---|
| `github.com/joomcode/errorx` | Stack-trace-enriched errors |
| `github.com/cenkalti/backoff/v4` | Exponential backoff in `Client` reconnect |
| `github.com/spf13/viper` | Config reading in `sip/` `TestMain` only |
| `github.com/stretchr/testify` | Test assertions |
