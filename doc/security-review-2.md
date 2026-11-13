# Security review 2: network-facing code not covered by the first two passes

## Scope, baseline, tools

- **Baseline:** `HEAD` was `d32205d` when the review started. `doc/security-scan.md` covers `bcb3954`. `git diff --stat bcb3954..HEAD -- '*.go'` showed only `cmd/gosip/{interactive,main}.go`, one interactive test, and two renamed test files; no production code in `sip/`, `ssh/` or `sercos/` had changed since the earlier review.
- **Reviewed here (not in the first two documents):** `ssh/ssh.go` (`AttachSIPSource`, per-channel `sip.Serve`), the `sip/` decoders and state machines (`server.go`, `conn*.go`, `timeoutreader.go`, every PDU `Read`), `sercos` value formatting and parsing of device-controlled bytes, the two local test binaries, and the PDU layouts against an independent reference.
- **Method:** fuzzing (Go `testing.F`, in-memory, no device), the race detector, scanners, and a comparison with the Bosch Rexroth S/IP reference code. Every finding below has a test that fails on the unfixed code.
- **Not done:** no hardware test (`sip/` tests need a device; none was configured); no fuzzing of the UDP broadcast path (`sip/udp.go`, `sip/broadcast.go`); no review of `cmd/gosip`; `sip/timeoutreader.go` was read but its behaviour after a timed-out read (the abandoned read goroutine can consume bytes of the next response) was not tested; the `gliderlabs/ssh` handshake has no timeout by default (see Info 4) and was not attacked beyond reading the source.

| Tool | Version | Result |
|---|---|---|
| Go | 1.27.4 darwin/arm64 | |
| `go vet ./...` | | clean |
| `golangci-lint run ./...` | 2.15.0 | 4 issues: `contextcheck` at `ssh/ssh.go:107,118` (the per-channel context is derived from the SSH context, false positive); `gosec` G115 at `sip/server.go:143-144` (triaged below). Tests are excluded by `.golangci.yaml`. |
| `gosec -quiet -fmt=json ./...` | dev build | 34 issues: G115 x29, G104 x5; triage below |
| `staticcheck ./...` | 2023.1.3 | **did not run**: `invalid Go version: "1.26.0"` (built for an older Go). Coverage of this scanner is missing. |
| `govulncheck ./...` | v1.9.0 | 0 called vulnerabilities; 1 module advisory, GO-2026-5932 (`golang.org/x/crypto/openpgp`, fixed in a pseudo-version after v0.58.0). `go list -deps ./...` lists no `openpgp` package, so it is not reachable. |
| `govulncheck -mode=binary` on a fresh `go build ./cmd/gosip` | v1.9.0 | no vulnerabilities |
| OSV API (`api.osv.dev/v1/query`) | | `golang.org/x/crypto` v0.58.0: GO-2026-5932 only. `gliderlabs/ssh` v0.3.8 and `modelcontextprotocol/go-sdk` v1.8.0: no advisories. |

### gosec triage (29 x G115, 5 x G104; line numbers at `d32205d`)

| Location | Conversion | Verdict |
|---|---|---|
| `sercos/data.go:214` | `uint64` -> `int64` | Intended two's-complement reinterpretation; sign-extended by the shift on the next lines. Safe. |
| `sercos/data.go:378` | `uint32` -> `int32` | Intended signed field (nanoseconds); the `switch` after it handles `|nanos| >= 1e9`. Safe. |
| `sercos/data_parse.go:180` | `uint32` -> `uint16` | Only reached for length 2, where structure fields are rejected (`parts[7] != ""`), so the value is `set<<12 \| number \| 0x8000` <= `0xFFFF`. Safe. |
| `sercos/data_parse.go:216, 229` (reported twice) | `int` -> `uint32` | `parseIDNField` (`:234`) bounds set <= 7, number <= 4095, structure fields <= 255. Safe. |
| `sercos/data_parse.go:268-269` | `int64` -> `int32` / `uint32` | nanos is in `[-1e9, 1e9)`; seconds is rejected above `MaxUint32` (`:264`). Safe. |
| `sercos/attribute.go:54,72,79` | `int` -> `uint32` | Masked to the field width on the same line. Safe. |
| `sercos/attribute.go:63` | `int` -> `uint32` | `length` is limited to 1, 2, 4, 8 two lines above. Safe. |
| `sip/readonlydata.go:14-15,90-91`, `readeverything.go:122-123`, `readdescription.go:14-15,109-110`, `readdatastate.go:13-14,68-69` | `int` -> `uint16` slave index and extension | **Not bounded** for the four `Conn` read methods: the value is caller-controlled, not wire-controlled, but it wrapped silently. Finding F-26 (fixed). The exported `Init` methods still truncate; they are lower-level API for already-validated values. |
| `sip/server.go:143-144` | `int64` -> `uint32` milliseconds | The values come from the upstream connection's `ConnectResponse` (`uint32` ms), so the round trip cannot overflow. Safe. |
| `sip/conn.go:304` | `int` -> `uint32` | `len(data) > 0xFFFF` is rejected at `:294`. Safe. |
| G104 x5 (`sip/server.go:42`, `sip/client.go:294,323,345,362`) | unchecked `Close()` | On paths that already failed or are tearing down. Info, no change. |

## Findings

IDs continue the numbering of `doc/security-scan.md`. Severity rule: High = unauthenticated remote crash/RCE/auth bypass; Medium = unauthenticated remote resource exhaustion measured above 100 MB or 1000 goroutines per attacker connection; Low otherwise. All six are fixed, each in its own commit.

| # | Severity | Area | Title | Status | Commit |
|---|---|---|---|---|---|
| 22 | Medium | `ssh` | One SSH connection can open an unbounded number of S/IP channels | Fixed | `91ca612` |
| 23 | Low | `sip` conn | A failed `Dial` leaks two goroutines per attempt | Fixed | `8186c1c` |
| 24 | Low | `sip` server | `Serve` spins on `Accept` errors and ignores a closed listener | Fixed | `30a33c4` |
| 25 | Low | `sip` server | Responses are written without a deadline; a peer that never reads holds a connection slot | Fixed | `745aedd` |
| 26 | Low | `sip` conn | Conn read methods truncate out-of-range slave index and extension | Fixed | `d2fcada` |
| 27 | Low | `sip` conn | `cleanUp` closes channels that the loops may still send on | Fixed | `bc27ae7` |

### 22. One SSH connection can open an unbounded number of S/IP channels

- **Severity:** Medium. Unauthenticated when the SSH server has no authentication handlers (the documented default of gliderlabs).
- **Location:** `ssh/ssh.go` `sipChannelHandler`.
- **Evidence:** every accepted `direct-tcpip` channel gets its own `sip.Serve` (and therefore its own 64 connection limit), a `net.Pipe`, and a bridge. `gliderlabs/ssh` starts each channel handler in its own goroutine and applies no channel limit (`server.go:318`). `TestSSHChannelFloodCost` measured, on one SSH connection with 1000 channels, 5000 goroutines running gosip server code (5.0 per channel: `Serve` accept loop, connection handler, two `io.Copy` goroutines, request discarder) and, with the in-process SSH client, +6011 goroutines and +75.6 MB heap (76 KB per channel, client and server together). 100 connections x 10 channels cost the same in total.
- **Reproducer:** `TestSSHChannelsPerConnectionAreBounded` (`ssh/ssh_channel_limit_test.go`). Before the fix: `"1000" is not less than or equal to "64"`. After: 32 channels opened, then `ssh: rejected: resource shortage ("too many channels")`, 160 goroutines.
- **Fix:** per-SSH-connection counter (`DefaultMaxChannelsPerConnection` = 32, `ext.WithMaxChannelsPerConnection`), released when the channel closes. `AttachSIPSource` gained a variadic `...AttachOption`, which is source compatible. README updated.
- **Residual:** the limit bounds channels per connection, not connections; `gliderssh.Server.IdleTimeout`/`MaxTimeout` default to zero (unlimited). Both are SSH server settings of the caller.

### 23. A failed `Dial` leaks two goroutines per attempt

- **Severity:** Low (two goroutines and one `conn` per failed attempt; a reconnecting `Client` repeats it with backoff).
- **Location:** `sip/conn.go` `dial`.
- **Evidence:** `dial` starts the send loop, the receive loop and the teardown watcher, then on a failed Connect exchange only called `cleanUp`, which closes the transport but never cancels the loop context. The send loop stays in `select` on the old request channel and the watcher waits for a cancel that never comes. Found by `FuzzClientAgainstHostileServer` (corpus entry `709b0fa2ae70e4f5`: a Connect response whose next frame has an unknown message type). Any failed connect (timeout, reset, invalid response) takes this path.
- **Reproducer:** `TestFailedDialDoesNotLeakGoroutines`. Before: `goroutines: 2 before, 42 after 20 failed dials`. After: `2 before, 2 after`.
- **Fix:** `Close` instead of `cleanUp`.

### 24. `Serve` spins on `Accept` errors

- **Severity:** Low (one core; stderr flood). [INFERENCE: an attacker exhausting file descriptors to cause EMFILE was not exercised.]
- **Location:** `sip/server.go` accept loop.
- **Evidence:** every `Accept` error was logged through the global `log` package and retried immediately. A listener closed by the caller without canceling the context (the usual way to stop a listener) was therefore retried forever. Measured with a listener whose `Accept` always fails: 29.9 and 29.9 million `Accept` calls in 300 ms.
- **Reproducer:** `TestServeAcceptErrorsDoNotSpin` (two subtests: `net.ErrClosed`, and an EMFILE-like error). Before: `29915845` / `29900020` calls. After: 1 / 6 calls.
- **Fix:** return on `net.ErrClosed`; back off 5 ms to 1 s on other errors (as `net/http` does); log through the package logger (disabled by default, per `AGENTS.md`).

### 25. Responses are written without a deadline

- **Severity:** Low (availability: 64 stuck connections stop `Serve`).
- **Location:** `sip/server.go` `writeWithHeader`.
- **Evidence:** the server set only a read deadline. A peer that connects and never reads blocks `connServer` in `Write` forever and keeps one of the 64 slots. With the synchronous `net.Pipe` this is immediate. Over TCP the kernel buffers must fill first: on macOS loopback 4.3 MB of pings sent in 4 s did not stall the connection, so a TCP attack needs a multi-megabyte request flood. [INFERENCE: Linux buffer sizes were not measured.]
- **Reproducer:** `TestServeSlowReaderDoesNotHoldConnectionSlotForever` (`WithMaxConnections(2)`, busy timeout 300 ms). Before: after 4 s `both server slots are still held by peers that never read their response`. After: passes in 1.0 s.
- **Fix:** write deadline of the busy timeout before each response.

### 26. Conn read methods truncate out-of-range slave index and extension

- **Severity:** Low (caller-controlled input, not wire-controlled).
- **Location:** `sip/conn.go` `ReadEverything`, `ReadOnlyData`, `ReadDescription`, `ReadDataState`.
- **Evidence:** `WriteData` and `Mux` validated 0-65535; the reads did not. `ReadOnlyData(65536, 0, ...)` was sent as slave 0, `-1` as slave 65535.
- **Reproducer:** `TestConnReadsRejectOutOfRangeSlaveAddress`. Before: four failures such as `ReadOnlyData(-1, 0) was sent to slave 65535 extension 0; err = <nil>`. After: all four return an error and nothing is sent.
- **Fix:** one `checkSlaveAddress` shared by the four reads and `WriteData`.

### 27. `cleanUp` closes channels that the loops may still send on

- **Severity:** Low (data race reported by the race detector on every `Close` with a request in flight; the panic itself was not observed in 9000 stress iterations without `-race`).
- **Location:** `sip/conn_connect_close.go` `cleanUp`, `sip/util.go` `signal`.
- **Evidence:** `cleanUp` closed `transactionStartedCh` and `concurrentTransactionLimitCh`. `signal` fetches the channel under a lock but sends after releasing it, so a close in between is a send on a closed channel, which panics in a loop goroutine and ends the process. Running the existing tests with `-race -count=10` reported five races, every one with the pair `cleanUp` (write) / `signal` (previous read) as top frames; tests that failed in that run include the existing `TestCanceledWriteDataDoesNotReplay` and, in `ssh/`, `TestSSHStalledFactoryDoesNotStarveOtherChannels`.
- **Reproducer:** `TestCloseWhileRequestInFlight` under `go test -race`. Before: two `DATA RACE` reports (`cleanUp` / `signal`), test fails. After: passes with `-race -count=3`. Without `-race` the test passes on the old code too, because the window is too narrow.
- **Fix:** the channels are no longer closed; `signal` and `wait` also select on `closedCh`.

## Info (no change)

1. **ConnectResponse allocation.** A 16-byte `ConnectResponse` with `NoMessageTypes = 0xFFFF` makes `Read` allocate 525,448 bytes (2 x 4 x 0xFFFF: the slice plus the byte buffer of `binary.Read`), and 262,140 bytes stay referenced in `connectResponse.MessageTypes`. The cap `0xFFFF` is loose for a list of at most about 30 defined message types, but the amplification is bounded and per connection. The fuzz target documents the exact budget.
2. **`DataLength` is not cross-checked** against `MaxListLength` or the attribute length. `FuzzDataFormatting` and `FuzzParseData` show the `sercos` formatters survive any combination (no panic, under 1 s).
3. **`AttachSIPSource` and a nil `LocalPortForwardingCallback`.** The gliderlabs `DirectTCPIPHandler` denies forwarding when the callback is nil; `sipChannelHandler` allows it and relies on the factory. Documented in the `AttachSIPSource` comment ("remains an additional filter"). `TestSSHFactoryReceivesAttackerControlledTargetVerbatim` shows the factory receives user, host (including NUL and path characters) and port (`0xFFFFFFFF`) exactly as sent: factories must validate them. The rejection text returned to the client is generic (`forwarding target rejected`); the factory's error text does not reach the client.
4. **`gliderlabs/ssh` v0.3.8 defaults.** `IdleTimeout` and `MaxTimeout` are zero (`server.go:55-56`); there is no handshake timeout in the server loop. Not changed by gosip; now stated in the README.
5. **Reference interop.** `SIPC_Receive.c:131-141` of the reference computes `noMessageTypes * 4` in `int` and allocates without a bound. gosip's own server sends at most six message types (`sip/server.go` `handleConnect`) and writes whatever lengths its source returns without a cap (read from `sip/server.go`, not tested); both stay far below the 32-bit overflow points of that code.
6. **Local test binaries.** `sip.test` (go1.24.1, 2025-03-22) and `sip_test.test` (go1.26.2, 2026-04-11) were never tracked (`git ls-files | grep '\.test$'` is empty, `git log --all -- sip.test sip_test.test` is empty); the installed toolchain is go1.27.4. `go version -m` lists no dependency modules in either (`mod` line only for the main module). A scan of `strings` output for `password|token|secret|PRIVATE KEY|10.x|192.168.x|172.16-31.x` found only standard-library identifiers; no private key blocks and no private IP addresses. Compared by package with a rebuild from the current tree (`go tool nm`), they lack only code that was added later (the `sercos` package, generic `Broadcast` and `MarshalPDU` instances, testify internals), and `sip_test.test` carries the former test package name `sip_test` where the tree now has `sip_test_test`. Both were deleted; git history was not rewritten (the 4.1 MB `gosip` binary from commit `20a57f0`, removed in `9604421`, remains in history).

## Negative results

Fuzz runs: `go test ./sip_test -run '^$' -fuzz=^FuzzX$ -fuzztime=60s`, 8 workers, on `d32205d` (decoders do not depend on the fixes above). Oracle for every decoder: no panic, allocation within the budget (4 x 0xFFFF + 64 KiB; ConnectResponse 8 x 0xFFFF + 64 KiB), and a successful `Read` re-encodes to exactly the bytes it consumed.

| Target | Time | execs | Result |
|---|---|---|---|
| `FuzzHeader` | 60 s | 1,538,625 | no crash |
| `FuzzConnectRequest` | 60 s | 1,441,592 | no crash |
| `FuzzConnectResponse` | 61 s | 1,178,180 | no crash (budget raised, Info 1) |
| `FuzzReadEverythingResponse` | 61 s | 1,374,260 | no crash |
| `FuzzReadOnlyDataResponse` | 60 s | 1,203,849 | no crash |
| `FuzzReadDescriptionResponse` | 61 s | 1,358,193 | no crash |
| `FuzzBrowseResponse` | 60 s | 1,339,724 | no crash |
| `FuzzSetIPRequest` | 60 s | 1,660,513 | no crash |
| `FuzzWriteDataRequest` | 60 s | 1,584,382 | no crash |
| `FuzzException` | 60 s | 1,587,598 | no crash |
| `sercos` `FuzzDataFormatting` (`String`, `Strings`, `Format` with 9 verbs x 7 widths, device-controlled attribute and bytes; < 1 s) | 61 s | 3,016,456 | no crash |
| `sercos` `FuzzParseData` (`ParseData`, `ParseDatas`, formatting round trip; < 1 s) | 61 s | 7,302,422 | no crash |
| `FuzzServeFrames` (arbitrary bytes behind a valid Connect; server still serves a Ping afterwards; goroutines back to baseline within 3 s), on the tree with F-22 to F-27 fixed | 61 s | 37,063 | no crash |
| `FuzzClientAgainstHostileServer` (arbitrary server stream; every call returns within 1 s of its 250 ms deadline; no goroutine left), after F-23 | 92 s | 370 | no crash. The first run, before the F-23 fix, found F-23. The low exec count is by design: each input may spend up to 3 x 250 ms in timeouts. |

Wire-length bounds (all before `make`; `maxPDUFieldLength` = `0xFFFF`, `sip/sip.go:25`):

| Field | Type | Bound check |
|---|---|---|
| `NoMessageTypes` | `uint32` | `sip/connect.go:66` |
| `NameLength`, `UnitLength` (ReadEverything) | `uint16` | type, plus `sip/readeverything.go:63,71` |
| `DataLength` (ReadEverything) | `uint32` | `sip/readeverything.go:79` |
| `NameLength`, `UnitLength` (ReadDescription) | `uint16` | type, plus `sip/readdescription.go:66,74` |
| `DataLength` (ReadOnlyData) | `uint32` | `sip/readonlydata.go:60` |
| `DisplayNameLength`, `HostNameLength` | `uint32` | `sip/browse.go:96,108` |
| `HostNameLength` (SetIP) | `uint32` | `sip/setip.go:63` |
| `DataLength` (WriteData) | `uint32` | `sip/write.go:25` |

Race detector: `go test ./sip_test ./ssh ./sercos -race -count=20` passes on the tree with F-22 to F-27 fixed (`sip_test` 199 s, `ssh` 51 s, `sercos` 1.5 s). The same command on the earlier tree failed on F-27. The later refactor commit only moves code into helpers; `go test ./ssh ./sip_test` passes after it and `-race` was re-run once (below).

Other negative results: a hostile server answering Connect with busy timeout 0 or 4294967295 ms does not make any public `Conn` call outlive its context deadline (seeds of `FuzzClientAgainstHostileServer`); `AttachSIPSource` teardown (client close, channel close, half-close, attach-context cancel) leaves no goroutines (`TestSSHTeardownLeavesNoGoroutines`, `TestSSHAttachContextCancelEndsChannels`); a stalled source factory does not block other channels of the same connection (`TestSSHStalledFactoryDoesNotStarveOtherChannels`); `LocalPortForwardingCallback` runs before the factory and a prohibition never reaches it (`TestSSHPortForwardingCallbackRunsBeforeFactory`).

## Cross-check against the Bosch Rexroth reference

Reference: SourceForge project "Sercos Internet Protocol Services" (LGPL 2.1; `SIP-POSIX.zip`, C client source; `Sip-14.8.0-develop00019.zip`, .NET `SIP.dll` binary with `SIP.xml` documentation). Downloaded to a temporary directory; nothing was copied into this repository (MIT) and `SIP.dll` was not executed. `SIPC_Struct.h` gives field order and widths; `SIP.xml` documents parameters and names only, no byte layouts.

| PDU | Reference | Verdict |
|---|---|---|
| ReadEverything request/response (69/70) | `SIPC_Struct.h`: slave u16, ext u16, IDN u32; response validelements, data_status, namelength u16, attribute u32, unitlength u16, min[8], max[8], maxlistlength u32, datalength u32, then name, unit, data | match |
| ReadOnlyData (71/72) | request as above; response attribute u32, length u32, data | match |
| ReadDescription (73/74) | response validelements, namelength u16, attribute u32, unitlength u16, min[8], max[8], maxlistlength u32, name, unit | match |
| ReadDataState (87/88) | response data_status u16 | match |
| Connect (63/64) | request version, busy, lease (u32); response adds noMessageTypes u32 and the u32 list | match |
| WriteData (83/84) | slave, ext, IDN, length u32, data; empty response | match |
| Exception (67) | common code u16, specific code u32 | match |
| ReadSegment (109/110) | request adds StartIndex u16, NumberOfRequestedElements u16; response attribute u32, NumberOfAvailableElements u16, length u32 | gosip declares the message types but implements no PDU; nothing to compare |
| Browse (91/100) | not in the C source; `SIP.xml` `SiPNode`: version, node identifier, MAC, DHCP features, DHCP mode, IP, subnet, gateway, display name, host name (lengths "in bytes") | field order matches; widths not documented in `SIP.xml`: partially unverifiable |
| SetIP (95/96) | `SetIpJob.Begin`: node identifier, MAC, DHCP mode (byte), IP, subnet, gateway, persistent (bool), host name | order matches; wire width of the persistent flag (gosip: 1 byte) unverifiable |
| Identify (93/94) | `IdentifyJob.Begin`: node identifier (MAC, 6 bytes) | match |

Message types: `grep -oE 'MessageType\([0-9]+\)' sip/message_types.go` against the numbers in `SIPC_Define.h`: all 49 values in the C header (61-110, including `EXCEPTION = 67`, `BUSY = 68`) equal gosip's. Go-only in 61-110: 98 (`ResetResponse`; `SIP.xml` lists `Reset` and `ResetResponse` by name without numbers). Outside the range, 113, 114, 131-142 are not in the C header and are not mentioned in `SIP.xml` (`RemoteService`, `ReadSymbol`, `ReadApplications`, `ReadNodes`, `WriteSymbol`: 0 hits), and 32768 `Echo` is listed in `SIP.xml` by name only: all of these are **unverifiable against the reference**, and no layout is asserted for them.

No parsing differential was found.

## Artifacts

New tests (all `go test`-safe, in-memory):

- `sip_test/fuzz_test.go`: ten decoder fuzz targets with the allocation and round-trip oracle.
- `sip_test/fuzz_state_test.go`: `FuzzServeFrames`, `FuzzClientAgainstHostileServer`.
- `sercos/fuzz_test.go`: `FuzzDataFormatting`, `FuzzParseData`.
- `ssh/ssh_security_test.go`: target forwarding, callback order, stalled factory, teardown, attach-context cancel, channel flood measurement.
- Reproducers: `ssh/ssh_channel_limit_test.go` (F-22, plus the option and its validation), `sip_test/security_failed_dial_test.go` (F-23), `sip_test/security_accept_loop_test.go` (F-24), `sip_test/security_write_stall_test.go` (F-25), `sip_test/security_slave_range_test.go` (F-26), `sip_test/security_close_race_test.go` (F-27).

Corpus: `sip_test/testdata/fuzz/FuzzClientAgainstHostileServer/709b0fa2ae70e4f5` (F-23). No other fuzz target produced a failing input. One `FuzzServeFrames` failure (`fuzzing process hung or terminated unexpectedly`) came from the test, not the library: the target called `Listener.Close()` itself, and `Serve` closes the same listener when its context is canceled, which panics in the in-memory test listener (double close). The same double close made the existing `TestSmoke` fail about once in 15 `-race` runs (`panic: close of closed channel`, `sip_test/server_test.go:33`). `Listener.Close` in `sip_test/listener_test.go` is now idempotent; the library is not involved, since `Serve` recovers from a panicking `Close`.
