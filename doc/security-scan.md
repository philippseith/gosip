# Repository security scan

## Run details

- Scanner: OMP Native Security
- Scan ID: `secscan_01a13b8e6b6b7152aabc8e7e3a7c6f0b`
- Target: full repository (`gosip`)
- Result: **7 findings** — 4 resolved, 3 open
- Coverage: **partial**
- Disposition: scan findings have not been independently exploit-validated.

The scan reviewed production sources, tests, documentation, configuration, histories/cache/IDE data, and module manifests. Variable-length PDU bounds were reviewed. It did not run builds, tests, lint, dynamic exploit checks, or an external dependency advisory lookup. The committed binaries `sip.test` and `sip_test.test` could not be inspected or verified against source; the scan left them as follow-up coverage.

A separate `govulncheck ./...` attempt did not produce a vulnerability result: the installed scanner was built with Go 1.21 source-processing packages and failed against the Go 1.27 `go list`/standard library. Dependency advisory status therefore remains unknown.

## Findings

### 1. Canceled request may be enqueued twice

- **Severity / confidence:** Medium / high
- **Rule / ID:** `gosip.conn-context-cancellation-replay` / `secf_334b0b0afc8b9f055eb5f071`
- **Locations:** `sip/conn_send_recv.go:334-350`, `sip/conn_send_recv.go:163-169`; supporting: `sip/conn.go:293-307`
- **Evidence:** `sendRequestWaitForResponseAndRead` enqueues the request before selecting on cancellation. Its cancellation branch enqueues the same PDU again with `context.WithoutCancel`. If the original response arrives, dispatch sends the callback to the original buffered channel and waits for it, while the canceled caller is waiting on the duplicate request channel. The receive loop can stall. Because `WriteData` uses this helper, the device write can also be repeated.
- **Status:** Fixed. Cancellation now drains the original request channel instead of enqueuing the same PDU again.
- **Conditions / impact:** Reachable through public `Conn` methods with a canceled `WithContext`; direct `Conn` calls do not necessarily close the connection on deadline. A side-effecting operation can be duplicated and response processing can stop.
- **Suggested fix:** Preserve and drain the original request channel after cancellation; do not enqueue a second PDU. Ensure a canceled caller cannot leave response dispatch waiting on an unconsumed callback.

### 2. Malformed response can leave a dead connection reusable

- **Severity / confidence:** Medium / high
- **Rule / ID:** `gosip.conn-dead-transport-reused-after-protocol-error` / `secf_8c230659dc09a9ef68ff93f3`
- **Locations:** `sip/conn_send_recv.go:63-67`, `sip/conn_send_recv.go:210-212`; supporting: `sip/conn.go:185-187,215-222`, `sip/client.go:319-321`, `sip/conn_send_recv.go:101-108`
- **Evidence:** An invalid response message type (including type 0) ends the receive loop and cancels its context. The watcher marks the connection closed/cancels requests but does not run `cleanUp`; `Connected` does not check the closed state. The transport and non-nil request queue can therefore remain. A later enqueue can synchronously block because the send loop has exited.
- **Status:** Fixed. Receive-loop failure now closes the transport and clears request state; health checks and request enqueue reject the dead connection.
- **Conditions / impact:** A peer that can send a malformed response while a request is pending may make subsequent client operations hang until explicit close or recovery.
- **Suggested fix:** Tear down and clear transport/queue state on receive-loop failure; include closed state in health checks; make enqueue context-aware so a dead send loop cannot block callers indefinitely.

### 3. TCP server forwards requests without client authorization

- **Severity / confidence:** Medium / high
- **Rule / ID:** `gosip.server-unauthenticated-data-access` / `secf_3415fb5a813f3a0a207636b8`
- **Locations:** `sip/server.go:17-20,46-64,107-121,127-151,197-202`; supporting: `sip/connect.go:14-21`
- **Evidence:** The documented example binds `:35021` (all interfaces). The connect exchange has no client identity/authentication fields. After accepting a connection, request dispatch forwards reads and `WriteData` directly to the configured `SyncClient`, with no per-client authorization in this path.
- **Conditions / impact:** If an application exposes the listener to an untrusted network and its source does not enforce operation authorization, an unauthenticated remote peer can read or modify device parameters. Exposure depends on downstream listener binding and source-side policy.
- **Suggested mitigation:** Authenticate/authorize before forwarding operations, or bind only to a trusted interface and enforce network access controls outside the package. Avoid wildcard exposure without a network boundary.

### 4. Server connection workers are unbounded by default

- **Severity / confidence:** Medium / high
- **Rule / ID:** `gosip.server-unbounded-connection-workers` / `secf_77b485364c41d9034ec39420`
- **Locations:** `sip/server.go:20-27,45-64,98-101`; supporting: `sip/conn_options.go:58-68`
- **Evidence:** `Serve` leaves `maxConnectionsCh` nil unless `WithMaxConnections` is supplied. The accept loop starts a goroutine for each accepted connection and skips the connection gate when the channel is nil. The 2-second read deadline bounds an idle worker's lifetime, but does not cap concurrent accepts.
- **Status:** Fixed. `Serve` now limits active connections to 64 by default; `WithMaxConnections` overrides the default.
- **Conditions / impact:** A reachable listener can be flooded with connections, consuming file descriptors, goroutines, and memory.
- **Suggested mitigation:** Apply a safe default cap or require a cap for network-facing use. Confirm the accept/back-pressure path remains bounded during overload.

### 5. Mux can share results across different authorization options

- **Severity / confidence:** Medium / medium
- **Rule / ID:** `gosip.mux-option-metadata-authorization-bypass` / `secf_1c84557ecfff48eb4a606777`
- **Locations:** `sip/mux.go:29-34,45-63,137-160`; supporting: `sip/requestoption.go:13-16,54-64`
- **Evidence:** The coalescing key contains the message type and request tuple, not request options/metadata. When a matching job exists, the later caller joins the first caller's work; the source operation is not invoked again with the later caller's options. Request options expose metadata.
- **Conditions / impact:** This can bypass per-caller authorization only if a consuming application uses request-option metadata for authorization and calls pass through `NewMux`.
- **Suggested fix:** Do not coalesce across distinct security contexts. Include authorization-relevant identity in the job key or bypass coalescing when caller-specific authorization options are present. Review downstream metadata use before choosing the compatibility behavior.

### 6. Malformed UDP replies can amplify memory use

- **Severity / confidence:** Medium / high
- **Rule / ID:** `gosip.broadcast-malformed-packet-memory-amplification` / `secf_a535aeeabf5d67f5814641aa`
- **Locations:** `sip/udp.go:22-48`; supporting: `sip/broadcast.go:34-44,107-126`
- **Evidence:** `listenUDP` accepts a maximum-size IPv4 UDP datagram and discards its sender address. If the message type matches but the body is malformed, it formats the full packet (`buf[:n]`) into an error sent to a buffered result channel. The scan estimates a burst could retain on the order of 100 MB. A blocked channel send also prevents the worker from noticing cancellation until results are drained.
- **Status:** Fixed. Parse errors now report only packet type and length; result delivery is cancellation-aware and the shared result buffer is capped at 64.
- **Conditions / impact:** A same-LAN peer that can observe the broadcast's ephemeral source port can send malformed expected-type replies during a broadcast, causing memory pressure and retaining a worker/socket.
- **Suggested fix:** Do not include full packet bytes in errors; report bounded metadata such as type and length. Bound retained result memory and make delivery cancellation-aware.

### 7. CLI metadata can inject terminal control sequences

- **Severity / confidence:** Low / high
- **Rule / ID:** `gosip.cli-terminal-control-injection` / `secf_eb73c98ae65ad46166a0a79c`
- **Locations:** `cmd/gosip/main.go:246-247,282-283,375-376`; peer-data parsing: `sip/browse.go:91-112`, `sip/readeverything.go:58-83`, `sip/readdescription.go:61-79`
- **Evidence:** The CLI prints peer-provided `Name`, `Unit`, `DisplayName`, and `HostName` as strings without terminal-safe escaping. `printData` sanitizes data values, but the metadata output path bypasses that handling.
- **Conditions / impact:** A malicious or compromised peer can forge terminal output or trigger terminal control behavior when the CLI output is viewed in a terminal.
- **Suggested fix:** Escape terminal ESC and other C0 control characters for all peer-controlled metadata before printing.

## Follow-up questions

1. Which downstream applications bind `sip.Serve` to untrusted interfaces, and do their `SyncClient` implementations enforce read/write authorization?
2. Do downstream applications use `RequestOption` metadata for per-caller authorization when calls pass through `NewMux`? If so, verify that coalescing cannot cross those caller identities.
3. Can the committed `sip.test` and `sip_test.test` binaries be provenance-checked or inspected with an available binary-aware tool? Do not assume they match the reviewed source.
4. Run dependency advisory analysis with a `govulncheck` build compatible with the installed Go toolchain; current scan results do not establish dependency CVE status.
