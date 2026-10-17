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

`ext.InstallSIPMux` installs a S/IP channel bridge into a caller-owned `gliderlabs/ssh` server. It passes a `net.Listener` implementation to `sip.Serve`; each authorized SSH `direct-tcpip` channel becomes an accepted S/IP connection. The bridge does not bind a TCP port or dial the requested destination. The host and port are logical forwarding values checked by the target callback.

Pass the S/IP mux, a slice of `sip.ConnOption`, and SSH options for the host signer, login callback, and target callback. The caller retains ownership of `gliderssh.Server` and starts it with `server.Serve`. The supplied context controls the S/IP `Serve` loop.

```go
package main

import (
	"context"
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
	server := &gliderssh.Server{}
	if err := ext.InstallSIPMux(
		ctx,
		server,
		sip.NewMux(source),
		[]sip.ConnOption{sip.WithMaxConnections(32)},
		ext.WithHostSigner(hostSigner),
		ext.WithLoginCallback(func(ctx context.Context, login ext.LoginInput) bool {
			return ctx.Err() == nil && login.Method == ext.LoginPassword &&
				login.Username == "operator" && login.Password == os.Getenv("SSH_PASSWORD")
		}),
		ext.WithTargetCallback(func(ctx context.Context, target ext.TargetRequest) bool {
			return ctx.Err() == nil && target.Username == "operator" &&
				target.Host == "127.0.0.1" && target.Port == 35021
		}),
	); err != nil {
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

The login callback receives each password or public-key attempt. The target callback authorizes each `direct-tcpip` channel before its stream is delivered to S/IP. Both callback contexts are scoped to the SSH connection, have no per-callback deadline, and must not be retained.

The installer reserves `direct-tcpip` for S/IP and rejects a server with an existing handler for that channel type. Other configured channel, request, and subsystem handlers are preserved. To support other applications, add their handlers to the same gliderlabs server; SCP/SFTP needs session or subsystem handlers, and a TFTP service needs a UDP gateway. The example uses a logical target: `ssh -p 2222 -N -L 35021:127.0.0.1:35021 operator@127.0.0.1` forwards each channel directly into `sip.Serve` without requiring a server-side TCP listener on port 35021.



