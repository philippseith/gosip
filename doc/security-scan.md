# Security scan and review

This document combines two assessments of `gosip`: an automated repository scan (OMP Native Security) and a manual review of the `develop` branch since it branched from `master`. Findings are numbered once, in one sequence: 1-7 come from the scan, 8-21 from the manual review. Finding 7 was reported by both and is merged. The summary table gives the number each finding had in its original document.

## Summary

- **21 findings:** 12 fixed, 2 mitigated or documented (9, 12), 1 accepted without change (19), 1 reclassified as device responsibility (8), 5 open (3, 5, 18, 20, 21).
- Open findings 3 and 5 depend on how downstream applications use the package and need a design decision; 18, 20 and 21 are low-severity items without a change yet.

| # | Severity | Area | Title | Status | Source |
|---|----------|------|-------|--------|--------|
| 1 | Medium | `sip` conn | Canceled request may be enqueued twice | Fixed | scan 1 |
| 2 | Medium | `sip` conn | Malformed response can leave a dead connection reusable | Fixed | scan 2 |
| 3 | Medium | `sip` server | TCP server forwards requests without client authorization | Open | scan 3 |
| 4 | Medium | `sip` server | Server connection workers are unbounded by default | Fixed | scan 4 |
| 5 | Medium | `sip` mux | Mux can share results across different authorization options | Open | scan 5 |
| 6 | Medium | `sip` UDP | Malformed UDP replies can amplify memory use | Fixed | scan 6 |
| 7 | Low-Medium | CLI/sercos | Terminal escape-sequence injection from device-supplied text | Fixed | scan 7, review 4 |
| 8 | Info (device scope) | MCP | `writedata` and `setip` perform no client-side authorization; enforcement belongs to the device | Reclassified | review 1 |
| 9 | Medium | MCP | Caller-chosen `address` allows probing and connecting from the host to any reachable TCP endpoint (SSRF-style, DNS exfiltration) | Mitigated by default | review 2 |
| 10 | Medium | MCP | `listen` has no upper bound; UDP result collection is unbounded | Fixed | review 3 |
| 11 | Medium | sercos | `Data.Signed` silently overflows when multiplying by the conversion factor | Fixed | review 5 |
| 12 | Medium | MCP | Device-controlled strings are returned verbatim to the LLM (prompt-injection channel) | Documented and marked | review 6 |
| 13 | Low | MCP | Unbounded client cache and un-normalised cache key | Fixed | review 7 |
| 14 | Low | MCP | Idle-eviction timer is not re-armed when the lease changes | Fixed | review 8 |
| 15 | Low | sercos | Misaligned list data is displayed as an empty value; factor 0 displays as `0` | Fixed | review 9 |
| 16 | Low | sercos | `Attribute.Format()` reads 4 bits; bit 23 may be reserved | Fixed (layout not verified) | review 10 |
| 17 | Low | CLI | `addressWithDefaultPort` mishandles `host:` and `:port` | Fixed | review 11 |
| 18 | Low | CLI | `--raw` writes without validation; the interactive `wr` command validates | Open | review 12 |
| 19 | Low | Prompt | Bracketed-paste terminator in pasted text ends paste mode early | No change | review 13 |
| 20 | Low | API | `sip.Idn` removed with no alias (breaking change for importers) | Open | review 14 |
| 21 | Info | Repo | `.vscode/launch.json` commits a lab IP address | Open | review 15 |

## Scope and method

### Repository scan (findings 1-7)

- Scanner: OMP Native Security
- Scan ID: `secscan_01a13b8e6b6b7152aabc8e7e3a7c6f0b`
- Target: full repository (`gosip`)
- Coverage: **partial**
- Disposition: scan findings have not been independently exploit-validated.

The scan reviewed production sources, tests, documentation, configuration, histories/cache/IDE data, and module manifests. Variable-length PDU bounds were reviewed. It did not run builds, tests, lint, dynamic exploit checks, or an external dependency advisory lookup. The committed binaries `sip.test` and `sip_test.test` could not be inspected or verified against source; the scan left them as follow-up coverage.

A separate `govulncheck ./...` attempt during the scan did not produce a vulnerability result: the installed scanner was built with Go 1.21 source-processing packages and failed against the Go 1.27 `go list`/standard library.

### Manual review of `develop` (findings 7-21)

- Range: `git diff master...HEAD` (merge-base `1a00978`), 18 commits, 30 files, about +5,480/-160 lines.
- Reviewed: `cmd/gosip/{main,mcp,interactive,write,hexdump}.go`, `sercos/{attribute,data,data_parse,data_scan,idn}.go`, the `sip/` changes (`conn.go` only; `idn.go` moved to `sercos`), `go.mod`, and `.vscode/launch.json`. Tests were read only where they affected a conclusion.
- Method: manual source review, plus a throwaway program in `/tmp` (deleted afterwards) that imported `sercos` to confirm findings 7, 11, 15, and 16. At the time of the review no repository file was changed; the fixes in the Status lines were made afterwards.
- `go mod verify`: all modules verified. `govulncheck ./...`: no called vulnerabilities. One module-level advisory, GO-2026-5932 in `golang.org/x/crypto@v0.58.0` (no fix listed), is not reachable from this code and is not introduced by this branch. This is a separate run from the failed scan attempt above; it was not repeated when the documents were combined.
- Not done: no hardware test, no fuzzing, no review of the pre-existing `sip/` protocol code beyond what the new MCP/CLI code calls. Findings 8-10 and 12-14 come from code reading only; they were not exercised against a live MCP client.
- Locations in findings 8-21 are the line numbers at review time and may have moved since; the Status lines describe the current code.

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
- **Status:** Open. No change in `sip/`.
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
- **Status:** Open. No change in `sip/`.
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

### 7. Terminal escape-sequence injection from device-supplied text

- **Severity:** Medium per the manual review (UDP Browse is unauthenticated, so any host on the broadcast domain can trigger it); Low / high confidence per the scan.
- **Rule / ID:** `gosip.cli-terminal-control-injection` / `secf_eb73c98ae65ad46166a0a79c`
- **Locations:** `cmd/gosip/main.go:372,375,408,411,504-505` (`Name`, `Unit`, `DisplayName`, `HostName`), `cmd/gosip/hexdump.go:61-90` (`sanitizeText`) and `writeElements`, `sercos/data.go:27-28,51-52,282-284` (`Text` format); peer-data parsing: `sip/browse.go:91-112`, `sip/readeverything.go:58-83`, `sip/readdescription.go:61-79`
- **Evidence:** Verified in the review: `sercos.Data("\x1b[2J\x1b]0;pwn\x07hi").Strings(textAttr)` returns the bytes unchanged. Before this branch, `printData` mapped bytes outside `0x20-0x7e` to `.`; the new `printData` calls `Strings(attribute)` and prints the result raw. `Name`, `Unit`, `DisplayName` and `HostName` were printed with `%s` and no filtering, so the metadata output path bypassed the data sanitizing.
- **Status:** Fixed. `sanitizeText` replaces control characters (C0, `0x7f`, C1, including newline and ESC) and bytes that are not valid UTF-8 with `.`. It is applied to `Name`, `Unit`, `DisplayName` and `HostName`, and, in `writeElements`, to every formatted element and limit (text data, `Min`, `Max`). The raw hex lines are unchanged. Covered by `TestSanitizeText` in `cmd/gosip/main_test.go`. MCP output is JSON and is not sanitized; finding 12 covers that channel.
- **Conditions / impact:** A malicious or compromised device (or any LAN host answering Browse) can clear the screen, set the window title, forge terminal output, or exploit terminal-specific escape handling when an operator runs `gosip` in a terminal.
- **Suggested fix:** Sanitize control characters (C0, `0x7f`, C1, and Unicode control characters) when printing device strings to a terminal. Keep the raw hex line (`Data (raw)`) as the faithful representation.

### 8. `writedata` and `setip` perform no client-side authorization (reclassified: device responsibility)

- **Severity:** Info. Originally rated High; reclassified after review feedback, which is accepted here.
- **Location:** `cmd/gosip/mcp.go:377-380` (tool registration), `:494-512` (`writeData`), `:593-630` (`setIP`).
- **Observation:** `writedata` accepts any `idn`, including P-parameters and command IDNs, plus `raw` hex. `setip` broadcasts a SetIP with optional `persist` to a node identified by MAC address. The MCP server enforces nothing itself; `writedata`, `identify`, and `setip` carry no `ReadOnlyHint`, and a client may ignore annotations anyway. The write path reads the current attribute first, so only the length is validated, not the value.
- **Why this is not a client defect:** S/IP is a device protocol, and the S/IP client is by design a tool that can issue any valid request. Authorization of changes (who may write parameters, start commands, or change the IP configuration) must be enforced by the device. A device that accepts unauthorized changes from any peer on the network is the non-conforming component, regardless of which client sends the request. This matches the EU Machinery Regulation (MVO, 2023/1230) and the Cyber Resilience Act (CRA), under which a product with digital elements must protect itself against unauthorized changes or lose its market access in the EU. Hardening one client cannot substitute for that, since any other S/IP client or a raw socket can send the same frames.
- **Residual note (optional, not a finding):** A client-side `--read-only` mode would only be a convenience that reduces accidental writes from agents; it is not a security control and is not recommended as a substitute for device-side enforcement. No change is requested.

### 9. Caller-chosen `address` allows probing and connecting to any reachable TCP endpoint

- **Severity:** Medium.
- **Location:** `cmd/gosip/mcp.go:57-61` (`clientCache.get`), `cmd/gosip/main.go:233-258` (`addressWithDefaultPort`), `:140-153` (dialer).
- **Evidence:** `address` is the only validation-free input that reaches `net.Dialer.DialContext("tcp4", address)`. Any host and port is accepted, not only 35021. The S/IP handshake bytes are sent to whatever answers. Host names are resolved by the local resolver.
- **Impact:** (a) Internal port and host scanning through different error text or timing. (b) A host name such as `<data>.attacker.example` leaks `<data>` through DNS. (c) Connecting to services on loopback or link-local ranges (for example cloud metadata) from the host running `gosip mcp`.
- **Recommendation:** Add an allow-list (`--allow-host` or CIDR list) enforced in `clientCache.get`, and consider restricting the port to 35021 unless explicitly widened. Resolve and check the IP, not only the string, before dialing.
- **Status (mitigated by default):** `clientCache.get` (`cmd/gosip/mcp.go`) now enforces two checks. (1) Port: only 35021 unless `gosip mcp --allow-port <port>` is given; empty, named, and non-numeric ports are rejected, which also covers the empty-port case of finding 17. (2) Host: the host must be a valid IPv4 literal (finding 13), so no name is ever resolved for agent input (closes DNS exfiltration), even with `--allow-all-hosts`; unless `gosip mcp --allow-all-hosts` is given, a device with that address must also answer a UDP Browse sent only on the interface the kernel routes to the address (`browseVerifier`, `interfaceForAddress`). Only addresses that pass can create a `clientCache` entry; the check runs again after an idle or closed client was removed. This closes port and host scanning of non-S/IP endpoints, loopback, and metadata addresses.
- **Remaining limits:** The Browse response address is self-reported, so a host on the same segment that spoofs a response can still get an address accepted. A failed verification costs up to 2 s and one UDP broadcast on one interface. Every address is verified the first time, so the backoff does not limit requests for new addresses: a caller that cycles through many different addresses causes one 2 s Browse per address. The backoff only stops repeated verification of an address that already failed: it is not verified again for 1 s, doubling per consecutive failure of any address up to 5 minutes, and a success forgets the failed addresses and resets the schedule (`checkVerifyBackoff`; a single `github.com/cenkalti/backoff/v4` `ExponentialBackOff` shared by all addresses, without jitter or give-up; the set of failed addresses is bounded to 256, an evicted address counts as new again). A global limit on verifications per window is not implemented. Devices behind routers and devices that do not answer a UDP Browse need `--allow-all-hosts`, which removes the Browse check; the address must still be an IPv4 literal.

### 10. `listen` has no upper bound; UDP result collection is unbounded

- **Severity:** Medium (availability of the MCP server).
- **Location:** `cmd/gosip/mcp.go:240-252` (`parseListen`), `:529-535`, `:584-590`, `:623-629`, `:538-552`, `:632-642`.
- **Evidence:** `parseListen` only requires `listen > 0`. `listen: "1000000h"` keeps the tool call and its UDP sockets open for that long (until the client cancels). `collectBrowseResults` appends every received response and every error string to slices with no cap, so a host on the LAN that floods spoofed UDP replies during the window can grow memory without limit.
- **Recommendation:** Clamp `listen` (for example 1-30 s), and cap the number of devices and errors collected.
- **Status (fixed):** `parseListen` (`cmd/gosip/mcp.go`) clamps `listen` to 10 s (longer values are silently reduced, not rejected). `collectBrowseResults` and `collectBroadcastResults` collect at most 1024 results (devices/responses and errors together); the 1025th cancels the listen context so the UDP producers stop, the rest is drained and dropped, and the output has `truncated: true`.

### 11. `Data.Signed` silently overflows when multiplying by the conversion factor

- **Severity:** Medium (wrong value shown to an operator; the unsigned path uses `big.Int` and is correct).
- **Location:** `sercos/data.go:197-203`.
- **Evidence:** Verified. For the 8-byte value `0x7fffffffffffffff` and factor 10, `Signed(8, 0, 10)` returns `-10`, while `Unsigned(8, 0, 10)` returns `92233720368547758070`. `value *= int64(factor)` wraps. Only 8-byte values with `|value| > MaxInt64/factor` are affected; 1-, 2-, and 4-byte values cannot overflow because the factor is at most 65535. The parse path (`fixedValue`) uses `big.Int`, so a displayed wrapped value no longer round-trips.
- **Recommendation:** Use `big.Int` (or a checked multiply) in `Signed`, as `Unsigned` already does.
- **Status (fixed):** `Data.Signed` multiplies with `big.Int`, like `Unsigned`. Tests cover `Signed(8, 0, 10)` for the largest and smallest int64 and a 4-byte value with factor 65535.

### 12. Device-controlled strings are returned verbatim to the LLM

- **Severity:** Medium (prompt-injection channel: injected instructions can steer the agent to call tools, in particular with a caller-chosen `address`, finding 9).
- **Location:** `cmd/gosip/mcp.go:554-566` (`newBrowseDevice`), `:644-668` (`descriptionValues`), `:204-210` (`newDataInfo`), `:538-552` (error strings).
- **Evidence:** `DisplayName`, `HostName`, IDN `Name`, `Unit`, text data elements, and wrapped network errors are returned as ordinary JSON strings. JSON encoding handles control characters but does nothing about natural-language instructions in the content.
- **Recommendation:** Document that tool output is untrusted; consider marking free-text fields in the output schema or description, and pair with finding 9 so that injected instructions cannot cause new connections without operator approval. Writes are constrained by the device, not by this client (finding 8).
- **Status (documented and marked):** the server instructions and the descriptions of `browse`, `readeverything`, `readonlydata` and `readdescription` state that device text is untrusted data and must not cause new tool calls, other parameters or writes (`untrustedNote` in `cmd/gosip/mcp.go`). The output schemas mark each device text field (`displayName`, `hostName`, `name`, `unit`, `elements`, `errors`) as untrusted. This is advice to the model, not enforcement; the enforcement is the address check of finding 9 and the client's approval of `writedata`, `identify` and `setip`, which are not annotated read-only.

### 13. Unbounded client cache and un-normalised cache key

- **Severity:** Low.
- **Location:** `cmd/gosip/mcp.go:57-105` (`get`), `cmd/gosip/main.go:233-258`.
- **Evidence:** Each distinct string produces a new `sip.Client` (socket, goroutines, timer) that is only evicted after `2 x lease` of idleness (default 20 s). The key is built from `addressWithDefaultPort(address)` without trimming or lowercasing, so `host`, `HOST`, `10.0.0.1`, and `010.0.0.1` map to separate clients for the same device, and `" 10.0.0.1"` (leading space, accepted by `required`, which trims only for its test) produces a failing key.
- **Recommendation:** Trim and canonicalize the key (lower-case host, resolved IP), and cap the number of simultaneous cached clients.
- **Status (fixed):** `parseMCPAddress` (`cmd/gosip/mcp.go`) accepts only a dotted-decimal IPv4 address (no leading zeros, no zone) with an optional decimal port of 1 to 65535, after trimming white space. Host names, IPv6, an empty host or port, named ports and trailing junk are rejected, with or without `--allow-all-hosts`. The cache key is the canonical `netip.AddrPort` string, so `10.0.0.5`, ` 10.0.0.5:035021` and `10.0.0.5:35021` share one client. The number of cached clients is capped at 1024 (`maxClients`): when it is reached the least recently used idle client is closed to make room, and if all 1024 are in use the request fails with `too many clients`.

### 14. Idle-eviction timer is not re-armed when the lease changes

- **Severity:** Low.
- **Location:** `cmd/gosip/mcp.go:88-100` (`release`), `:107-135` (`idleTimeout`, `evictIdleLocked`).
- **Evidence:** The timer is armed at `release` with `2 x LeaseTimeout()` at that moment, falling back to `defaultLease` when the lease is 0 (a request that failed before connecting). `evictIdleLocked` recomputes the timeout when the timer fires; if the lease has grown in the meantime, the entry is skipped and the timer is not re-armed. The entry (and its TCP connection) then stays open until some later `get` call runs `evictIdleLocked`, or until shutdown. [INFERENCE: not exercised; depends on a lease negotiated after the first release.]
- **Recommendation:** Re-arm the timer in `evictIdleLocked` for entries that are idle but not yet expired.
- **Status (fixed):** `evictIdleLocked` resets the timer of an idle client that is not yet expired to its remaining idle time, using the current lease. A test lets the lease grow after the timer was armed and checks that the timer evicts the client.

### 15. Misaligned list data is shown as empty; factor 0 shows `0`

- **Severity:** Low.
- **Location:** `sercos/data.go:137-152, 170-185, 207-222, 303-318, 336-351, 374-384` (the `Strings`-family helpers return `nil` when `len(d) % length != 0`), `sercos/data.go:155-167`.
- **Evidence:** Verified. `Data{1,2,3,4,5}.Strings(unsigned 4-byte)` returns `nil`. `writeElements` then prints an empty `Data:` line, and MCP `newDataInfo` returns `elements: []`, so a device returning a truncated or oddly sized list looks like an empty list, with only the raw hex line (not present in the MCP elements) revealing the data. For attribute factor 0, `Data{5,0}.String(attr)` returns `0` regardless of content, although `fixedValue` rejects factor 0 for parsing.
- **Recommendation:** Return an explicit marker (for example `invalid length 5 for 4-byte elements`) instead of `nil`, and treat factor 0 as an invalid attribute in display as the parser already does.
- **Status (fixed):** every `Strings`-family helper (`Bins`, `Unsigneds`, `Signeds`, `Hexen`, `Idns`, `Floats`, `Times`) returns one message such as `invalid length 5 for 4-byte elements` instead of `nil` for data whose length is not a multiple of the element length; it appears in the CLI `Data:` line and in the MCP `elements`. A conversion factor of 0 is displayed as `invalid conversion factor 0` by `Unsigned`, `Signed`, `Unsigneds` and `Signeds`, as the parser already rejects it.

### 16. `Attribute.Format()` reads four bits

- **Severity:** Low. [INFERENCE: depends on the S-0-0x attribute layout, not checked against the standard here.]
- **Location:** `sercos/attribute.go:28-30` (`0x00F00000`), `:68-73`.
- **Evidence:** Verified: attribute `0x00910001` yields format 9 and the string `invalid format 9`. If bit 23 is reserved (my recollection of the Sercos attribute layout), a device that sets it would be displayed and parsed as an invalid format, which also blocks formatted writes in `encodeWrite`. The MCP `newAttributeInfo` bounds-checks the name lookup, so there is no panic.
- **Recommendation:** Check the layout against the Sercos specification; if bit 23 is reserved, mask three bits.
- **Status (fixed, layout not verified against the specification):** `Format()` and `WithFormat` use bits 22-20 only; bit 23 is ignored. I could not obtain the Sercos specification text for the attribute layout (the public manuals and the OPC UA for Sercos pages describe the display formats but not the bit positions). The change rests on two points: the eight formats `Bin` to `Time` that the code defines fit exactly in three bits, and my recollection that bit 23 is reserved. If bit 23 has a meaning on some device, it is now just not read as part of the format. An `Attribute` can no longer yield an invalid format, so the `invalid format` default branches in `Data.String`, `Data.Strings` and `ParseData` are only a guard.

### 17. `addressWithDefaultPort` mishandles `host:` and `:port`

- **Severity:** Low.
- **Location:** `cmd/gosip/main.go:233-258`.
- **Evidence:** Read from the code: `net.SplitHostPort("host:")` and `net.SplitHostPort(":35021")` succeed, so both are returned unchanged. `host:` then dials with an empty port instead of defaulting to 35021, and `:35021` dials the local machine (Go treats an empty host as local), which for MCP passes `required` (finding 9).
- **Recommendation:** Reject an empty host or empty port, or default the empty port.
- **Status (fixed):** `addressWithDefaultPort` returns an error for an empty address, an empty host (`:35021`) and an empty port (`host:`). `--address` and the prompt command `a` report it. Host names stay valid for the CLI; the MCP server accepts IPv4 literals only (finding 13).

### 18. `--raw` writes without validation; interactive `wr` validates

- **Severity:** Low (behavioral inconsistency; the CLI path is a deliberate, documented bypass).
- **Location:** `cmd/gosip/main.go:416,425-431`, `cmd/gosip/write.go:81-100`, `cmd/gosip/interactive.go:824-828`.
- **Evidence:** `gosip writedata --raw` writes the bytes without reading the attribute, so any length is sent. The interactive `wr` and MCP `raw: true` call `encodeWrite`, which reads the IDN first and enforces `checkWriteLength`. `checkWriteLength` returns early for `Text` format, and for list data it only checks divisibility, so oversized text or list writes are not bounded before the device rejects them.
- **Recommendation:** Pick one behavior, or document the difference in the README; cap the maximum write size.
- **Status:** Open.

### 19. Bracketed-paste terminator in pasted text ends paste mode early

- **Severity:** Low (most terminals strip `ESC[201~` from pasted data themselves).
- **Location:** `cmd/gosip/interactive.go:377-394` (`readPaste`).
- **Evidence:** An embedded `ESC[201~` ends the paste, and the bytes after it are handled as typed input, including `\r`, which submits the line. The per-line conversion of newlines to spaces then no longer protects against a pasted multi-command payload (for example `w ...`). [INFERENCE: not tested in a terminal.]
- **Recommendation:** Leave as is if the supported terminals filter the sequence; otherwise discard everything up to the next newline after an unexpected early terminator.
- **Status (no change):** the embedded terminator cannot be told from a real one without timing, because bytes after a real `ESC[201~` can also arrive in the same read: scripted input such as `ESC[200~text ESC[201~ CR` is a legitimate pattern, and the tests use it. Discarding input up to the next newline after any terminator would break it, and a delay heuristic would be unreliable. The recommendation's first branch applies: the terminals that bracket pastes (xterm, VTE, iTerm2, Windows Terminal, kitty, tmux) are expected to remove the sequence from pasted text, but I did not test that in a terminal. A terminal that does not filter it still allows a crafted clipboard to submit a command at the `gosip>` prompt; the prompt takes only read, write, address and browse commands, and the same text pasted into a shell prompt is the larger exposure.

### 20. `sip.Idn` removed with no alias

- **Severity:** Low (API compatibility).
- **Location:** `sip/idn.go` -> `sercos/idn.go`, `sip/conn.go:313`.
- **Evidence:** `sip.Idn` no longer exists. The repository deliberately uses a clean cutover (no shims), and all in-repo callers were migrated, but any external importer of `sip.Idn` breaks at compile time.
- **Recommendation:** State the change in release notes; bump the module version accordingly.
- **Status:** Open.

### 21. `.vscode/launch.json` commits a lab IP address

- **Severity:** Info.
- **Location:** `.vscode/launch.json:11`.
- **Evidence:** Contains `192.168.112.67` and a test IDN. No credentials.
- **Recommendation:** Remove the file from the branch or replace the address with a placeholder.
- **Status:** Open.

## Reviewed with no issue found

- `clientCache` locking: dialing (lazy `NewClient`) and `Closed()` run under `c.mu`; `Close` of evicted and replaced clients happens outside the lock; the `release` closure is idempotent through `sync.Once`.
- MCP stdout: the library packages write nothing to stdout, and S/IP logging goes to stderr, so the stdio protocol stream is not corrupted.
- `lineReader` editing operations (`insertPaste`, `backspace`, `applyCursorKey`): index arithmetic is correct. The cursor remains within `[0, len(line)]`; non-ASCII input is dropped.
- Slave and extension ranges are validated in `mcpTools.target` and again in `conn.WriteData`.
- `parseTime`, `encodeInt`, `fixedValue`: range checks use `big.Int`; out-of-range values are rejected.
- `parseIdn` numeric handling: decimal and `0x` hex are explicit, and `S30` is accepted only for the standard (S) type.
- The earlier dial-context lifetime problem (a 5 s context created before `NewClient` and canceled by `defer`) is gone: the dialer now uses `net.Dialer.Timeout`.

## Follow-up questions

1. Which downstream applications bind `sip.Serve` to untrusted interfaces, and do their `SyncClient` implementations enforce read/write authorization? (finding 3)
2. Do downstream applications use `RequestOption` metadata for per-caller authorization when calls pass through `NewMux`? If so, verify that coalescing cannot cross those caller identities. (finding 5)
3. Can the committed `sip.test` and `sip_test.test` binaries be provenance-checked or inspected with an available binary-aware tool? Do not assume they match the reviewed source.
4. The scan could not run `govulncheck`; the manual review reports a successful run with no called vulnerabilities and one unreachable advisory (GO-2026-5932). Re-run it with a build compatible with the installed Go toolchain to confirm the current dependency status.
5. Is the Sercos attribute layout (bit 23 reserved) confirmed in the specification? (finding 16)

## Changes since the original documents

Verified in the source when the documents were combined: `git diff HEAD --stat -- sip` was empty, so no `sip/` finding changed after the scan.

| # | Finding | Scan status | Now |
|---|---------|-------------|-----|
| 7 | Terminal escape-sequence injection | Open (scan) | Fixed (`sanitizeText` on all metadata and element output) |

Scan result line: 5 of 7 scan findings resolved (1, 2, 4, 6, 7), 2 open (3, 5). Findings 1, 2, 4 and 6 were already marked fixed in the scan document and are unchanged. The tools were not re-run when the documents were combined.
