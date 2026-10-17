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

## Restricted SSH forwarding

The `ext` package provides an SSH server for local TCP forwarding. It requires a host signer, a login callback, and a target callback. Only SSH `direct-tcpip` channels are enabled; shell, exec, subsystem, remote-forwarding, and non-authorized targets are rejected. The login callback receives each username and either a password or public key; the target callback receives the authenticated username and requested host and port.

```go
package main

import (
	"context"
	"net"
	"os"

	"github.com/philippseith/gosip/ext"
	cryptossh "golang.org/x/crypto/ssh"
)

func serveSSH() error {
	hostKeyPEM, err := os.ReadFile("ssh_host_ed25519")
	if err != nil {
		return err
	}
	hostSigner, err := cryptossh.ParsePrivateKey(hostKeyPEM)
	if err != nil {
		return err
	}
	server, err := ext.NewSSHServer(
		ext.WithHostSigner(hostSigner),
		ext.WithLoginCallback(func(ctx context.Context, login ext.LoginInput) bool {
			return ctx.Err() == nil && login.Method == ext.LoginPassword &&
				login.Username == "operator" && login.Password == os.Getenv("SSH_PASSWORD")
		}),
		ext.WithTargetCallback(func(ctx context.Context, target ext.TargetRequest) bool {
			return ctx.Err() == nil && target.Username == "operator" &&
				target.Host == "192.168.1.50" && target.Port == 35021
		}),
	)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:2222")
	if err != nil {
		return err
	}
	return server.Serve(listener)
}
```

Callbacks run for each input. Their context is scoped to the SSH connection, not an individual forwarding channel, and has no per-callback deadline. Derive a timeout for blocking external checks, pass the derived context to context-aware dependencies, and return `false` when the context is canceled. Do not retain the context after the callback returns.

An SSH client can forward the authorized target with `ssh -p 2222 -N -L 35021:192.168.1.50:35021 operator@127.0.0.1`.



