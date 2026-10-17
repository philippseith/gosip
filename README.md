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

The `ext` package installs the S/IP endpoint into a caller-owned `gliderlabs/ssh` server. `InstallSIPMux` creates a loopback TCP listener and passes it to a `Serve` callback; that callback chooses the mux, its `SyncClient`, and `sip.Serve` options. The function returns the listener for address discovery and lifecycle management. Configure SSH authentication, target checks, channel handlers, and the SSH `Serve` call directly on `gliderssh.Server`.

If the server has no `LocalPortForwardingCallback`, the installer permits only the exact loopback S/IP listener target. An existing callback is preserved and must authorize that target itself. If `ChannelHandlers` is nil, only `direct-tcpip` is enabled; preconfigured handlers are preserved, so applications can add their own channels and requests.

```go
package main

import (
	"context"
	"fmt"
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
	sipListener, err := ext.InstallSIPMux(server, func(listener net.Listener) error {
		addr, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			return fmt.Errorf("unexpected S/IP listener address %T", listener.Addr())
		}
		server.LocalPortForwardingCallback = func(ctx gliderssh.Context, host string, port uint32) bool {
			return ctx.Err() == nil && ctx.User() == "operator" &&
				host == addr.IP.String() && port == uint32(addr.Port)
		}
		return sip.Serve(ctx, listener, sip.NewMux(source), sip.WithMaxConnections(32))
	})
	if err != nil {
		return err
	}
	defer sipListener.Close()
	fmt.Printf("S/IP forwarding target: %s\n", sipListener.Addr())

	sshListener, err := net.Listen("tcp", "127.0.0.1:2222")
	if err != nil {
		return err
	}
	defer sshListener.Close()
	return server.Serve(sshListener)
}
```

The authentication and forwarding callbacks receive a context scoped to the SSH connection, not an individual attempt or channel; there is no per-callback deadline. Derive a timeout for blocking external checks and do not retain the context after the callback returns.

The SSH server and mux are independent of the S/IP installer, so the same server can expose other TCP applications through its own forwarding policy. Add application-specific channel handlers as needed. Native SCP/SFTP requires session or subsystem handlers; TFTP uses UDP and needs a separate gateway. The S/IP target above is forwarded with `ssh -p 2222 -N -L 35021:127.0.0.1:<SIP-port> operator@127.0.0.1`, where `<SIP-port>` is the port printed by the server.



