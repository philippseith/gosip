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
