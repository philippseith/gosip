# gosip
This package implements S/IP, the Sercos Internet Protocol for accessing data in Sercos devices, in go.

Supported services:
- TCP Ping to S/IP ping the device.
- TCP ReadEverything, ReadDescription, ReadOnlyData, ReadDataState for reading parameter data 
- TCP WriteData for writing parameter data
- UDP Browse to browse for devices supporting the S/IP protocol.
- UDP SetIP and Identify to set the IP of a S/IP device or enable the identify signal on the device.

## Install the gosip CLI

To install the gosip CLI tool into your Go toolchain, run:

```bash
go install github.com/philippseith/gosip/cmd/gosip@latest
```

Use a tagged version instead of `@latest` if you want to pin a specific release, for example `@v0.1.0`.

### Basic usage examples

The CLI supports a simple TCP ping check as well as UDP broadcast helpers for identification and network configuration.

To ping a device at its S/IP address:

```bash
gosip --address 192.168.1.50:35021 ping
```

The `--address` (`-a`) flag accepts a host or IP without a port; it defaults to the S/IP port `35021`. Specify `host:port` to use a different port.

To read the full data set for an IDN, including metadata and values:

```bash
gosip --address 192.168.1.50:35021 readeverything --idn S-0-0095.0.0
```

To read only the raw data bytes of an IDN:

```bash
gosip --address 192.168.1.50:35021 readonlydata --idn 0x100A
```

Read output includes formatted values and spaced raw bytes. Long values wrap to the terminal width; wrapped raw rows show hexadecimal byte offsets, and wrapped formatted rows show decimal element indexes. Indexes are omitted when values fit on one row.

Formatted writes accept values as printed by `readonlydata`, with list elements separated by spaces. To write raw parameter data as a hexadecimal byte string, use `--raw`:

```bash
gosip --address 192.168.1.50:35021 writedata --idn S-0-0095.0.0 --raw --data 00112233
```

To identify a device on a specific network interface:

```bash
gosip identify --interface eth0 --node 00:11:22:33:44:55
```

To configure the IPv4 address of a device on that same interface:

```bash
gosip setip --interface eth0 --node 00:11:22:33:44:55 --ip 192.168.1.100 --gateway 192.168.1.1 --persist
```

The `--interface` flag selects the local NIC used for the broadcast, and `--node` is the device node identifier to target.

## Interactive prompt

Running `gosip` without a command in a terminal opens the read/write prompt. Up and Down recall up to 500 in-memory lines; Left/Right/Home/End move the cursor, Delete and Backspace edit at the cursor, and Ctrl-A/B/E/F provide cursor movement. Bracketed terminal paste inserts the pasted text as one line, converting line breaks and tabs to spaces. History is separate for the `gosip>` and `idn>` prompts and is not saved to disk.

## MCP stdio server

Run `gosip mcp` to expose the CLI operations as MCP tools over standard input and output. Pass `address` in every device-tool request (host[:port], default port 35021); `gosip mcp --address` is rejected. The tools are `ping`, `readeverything`, `readonlydata`, `readdescription`, `readdatastate`, `writedata`, `browse`, `identify`, and `setip`. Results are structured JSON; the server keeps one client per normalized device address and closes idle clients after twice their negotiated lease timeout (20 seconds before a lease is negotiated, unless `--leasetimeout` overrides it). Broadcast tools use UDP; `browse` without `interface` lists available interfaces, while `identify` and `setip` require it.

## Parsing formatted Sercos data

`sercos.ParseData(attr, s)` reverses `Data.String(attr)`. `sercos.ParseDatas(attr, ss)` parses the elements returned by `Data.Strings(attr)` and concatenates their bytes. `sercos.Data` also implements `fmt.Scanner` for `b`, `u`, `d`, `x`, `X`, `s`, `i`, `f`, and `t`.

```go
func parseAndScan() error {
	attr := sercos.Attribute(0x03110001)
	data, err := sercos.ParseData(attr, "1.000")
	if err != nil {
		return err
	}
	var scanned sercos.Data
	if _, err = fmt.Sscanf("0b0000.0000.0000.0010", "%b", &scanned); err != nil {
		return err
	}
	fmt.Printf("parsed=%v scanned=%v\n", []byte(data), []byte(scanned))
	return nil
}
```

For `b`, `u`, `d`, `x`, `X`, `i`, and `f`, scanning reuses an existing `Data` length of 1, 2, 4, or 8 bytes; use an empty `Data` to infer the length from the token. `s` and `t` ignore the current length. `fmt` width keeps its standard meaning as a maximum input-rune count, not a byte length.

## Protocol limits

Per the Sercos IDN parameter model, this implementation enforces a maximum of 65535 (0xFFFF) bytes
on all variable-length PDU fields:

| Field | PDU(s) |
|---|---|
| Data | ReadEverything, ReadOnlyData, WriteData |
| Name | ReadEverything, ReadDescription |
| Unit | ReadEverything, ReadDescription |
| DisplayName, HostName | Browse |
| NoMessageTypes | Connect |

Responses advertising larger values are rejected with an error.

## SSH server integration

`ext.AttachSIPSource` adds a S/IP bridge to a caller-owned `gliderlabs/ssh` server. It installs a `net.Listener` that accepts streams from SSH `direct-tcpip` channels and calls `sip.Serve` for each channel. No TCP listener or destination dial is created.

The caller configures host keys and authentication directly on `gliderssh.Server`. Authentication is optional to `AttachSIPSource`: gliderlabs permits unauthenticated SSH when no authentication handlers are configured. Use that only on a trusted network or listener. The factory can reject destinations, but `TargetRequest.Username` is not an authenticated identity unless server authentication is configured.

```go
package main

import (
	"context"
	"errors"
	"net"
	"os"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/philippseith/gosip/ext"
	"github.com/philippseith/gosip/sip"
	cryptossh "golang.org/x/crypto/ssh"
)

func serveSSH(ctx context.Context, source sip.SyncClient) error {
	hostKeyPEM, err := os.ReadFile("ssh_host_ed25519")
	if err != nil {
		return err
	}
	hostSigner, err := cryptossh.ParsePrivateKey(hostKeyPEM)
	if err != nil {
		return err
	}
	server := &gliderssh.Server{
		HostSigners: []gliderssh.Signer{hostSigner},
		PasswordHandler: func(ctx gliderssh.Context, password string) bool {
			return ctx.Err() == nil && ctx.User() == "operator" && password == os.Getenv("SSH_PASSWORD")
		},
	}
	mux := sip.NewMux(source)
	factory := func(ctx context.Context, target ext.TargetRequest) (sip.SyncClient, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if target.Username != "operator" || target.Host != "127.0.0.1" || target.Port != 35021 {
			return nil, errors.New("forwarding target is not allowed")
		}
		return mux, nil
	}
	if err := ext.AttachSIPSource(ctx, server, factory, []sip.ConnOption{sip.WithBusyTimeout(3000)}); err != nil {
		return err
	}
	sshListener, err := net.Listen("tcp", "127.0.0.1:2222")
	if err != nil {
		return err
	}
	defer sshListener.Close()
	return server.Serve(sshListener)
}
```

The factory is called concurrently and should be safe for concurrent use. Its parameter is the standard `context.Context`, derived from the SSH connection and canceled when that connection or the attachment stops. `gliderssh.Context` is the richer context supplied to gliderlabs callbacks: it embeds `context.Context` and adds SSH-specific metadata and synchronization methods. The factory receives the standard interface for decoupling; `TargetRequest` carries the requested username and forwarding destination.

The installer preserves the server's other channel, request, and subsystem handlers, including gliderlabs' default session handler when no channel map was supplied. It reserves `direct-tcpip`; attaching to a server with an existing handler for that type returns an error. SCP/SFTP can use configured session or subsystem handlers; a TFTP service needs a UDP gateway. The example's `127.0.0.1:35021` is a logical SSH target, not a server-side TCP listener: `ssh -p 2222 -N -L 35021:127.0.0.1:35021 operator@127.0.0.1` sends each channel directly to its factory-selected S/IP service.



