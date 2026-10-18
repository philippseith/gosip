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

To read the full data set for an IDN, including metadata and values:

```bash
gosip --address 192.168.1.50:35021 readeverything --idn S-0-0095.0.0
```

To read only the raw data bytes of an IDN:

```bash
gosip --address 192.168.1.50:35021 readonlydata --idn 0x100A
```

To write raw parameter data as a hexadecimal byte string:

```bash
gosip --address 192.168.1.50:35021 writedata --idn S-0-0095.0.0 --data 00112233
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

The caller configures host keys and authentication directly on `gliderssh.Server`. `AttachSIPSource` receives a factory and a slice of `sip.ConnOption`. The factory runs once per `direct-tcpip` channel and returns the `sip.SyncClient` for that channel; it may return a source or a mux such as `sip.NewMux(source)`. Return an error to reject a target. The optional server `LocalPortForwardingCallback` is also checked before the factory.

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

The factory is called concurrently and should be safe for concurrent use. Its parameter is the standard `context.Context`, derived from the SSH connection and canceled when that connection or the attachment stops. `gliderssh.Context` is the richer context supplied to gliderlabs callbacks: it embeds `context.Context` and adds SSH-specific metadata and synchronization methods. The factory receives the standard interface for decoupling; `TargetRequest` carries the authenticated username and forwarding destination.

The installer preserves the server's other channel, request, and subsystem handlers, including gliderlabs' default session handler when no channel map was supplied. It reserves `direct-tcpip`; attaching to a server with an existing handler for that type returns an error. SCP/SFTP can use configured session or subsystem handlers; a TFTP service needs a UDP gateway. The example's `127.0.0.1:35021` is a logical SSH target, not a server-side TCP listener: `ssh -p 2222 -N -L 35021:127.0.0.1:35021 operator@127.0.0.1` sends each channel directly to its factory-selected S/IP service.



