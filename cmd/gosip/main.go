// Command gosip is a command line client for the Sercos Internet Protocol (S/IP).
//
// It creates a sip.Client with sip.NewClient and exposes every method of
// sip.SyncClient as a subcommand. The UDP broadcasts sip.Browse, sip.Identify
// and sip.SetIP are available as subcommands as well.
//
// Usage:
//
//	gosip [global flags] <command> [command flags]
//
// Commands:
//
//	ping             Send a S/IP Ping
//	readeverything   Read description, data state and data of an IDN
//	readonlydata     Read only the data of an IDN
//	readdescription  Read only the description of an IDN
//	readdatastate    Read only the data state of an IDN
//	writedata        Write data to an IDN
//	browse           Broadcast a Browse request and list the answering devices
//	identify         Broadcast an Identify request
//	setip            Broadcast a SetIP request
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/philippseith/gosip/sip"
	"github.com/spf13/pflag"
)

type globalFlags struct {
	address      string
	busyTimeout  int
	leaseTimeout int
	keepAlive    bool
	dialTimeout  time.Duration
	timeout      time.Duration
	retries      uint
	verbose      bool
}

func newFlagSet(name string) *pflag.FlagSet {
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.SortFlags = false
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: %s [flags]\n", name)
		fs.PrintDefaults()
	}
	return fs
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error { // nolint:cyclop
	g, fs, err := parseGlobalFlags(args)
	if err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errors.New("no command given")
	}

	sip.EnableLogging(g.verbose)

	cmd, cmdArgs := fs.Arg(0), fs.Args()[1:]

	// Browse, Identify and SetIP are UDP broadcasts and need no client.
	switch cmd {
	case "browse":
		return doBrowse(cmdArgs)
	case "identify":
		return doIdentify(cmdArgs)
	case "setip":
		return doSetIP(cmdArgs)
	}

	connOptions := []sip.ConnOption{}
	if g.busyTimeout > 0 {
		connOptions = append(connOptions, sip.WithBusyTimeout(g.busyTimeout))
	}
	if g.leaseTimeout > 0 {
		connOptions = append(connOptions, sip.WithLeaseTimeout(g.leaseTimeout))
	}
	if g.keepAlive {
		connOptions = append(connOptions, sip.WithSendKeepAlive())
	}
	dialCtx, cancelDial := context.WithTimeout(context.Background(), g.dialTimeout)
	defer cancelDial()
	connOptions = append(connOptions, sip.WithDialContext(dialCtx))

	client, err := sip.NewClient("tcp4", g.address, connOptions...)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	reqOptions := []sip.RequestOption{sip.WithRetries(g.retries)}
	if g.timeout > 0 {
		reqOptions = append(reqOptions, sip.WithTimeout(g.timeout))
	}

	cmdErr := runClientCommand(client, cmd, cmdArgs, reqOptions, fs)
	if !errors.Is(cmdErr, pflag.ErrHelp) {
		fmt.Printf("BusyTimeout: %v\n", client.BusyTimeout())
		fmt.Printf("LeaseTimeout: %v\n", client.LeaseTimeout())
	}
	return cmdErr
}

func runClientCommand(client sip.Client, cmd string, cmdArgs []string, reqOptions []sip.RequestOption, fs *pflag.FlagSet) error {
	switch cmd {
	case "ping":
		return doPing(client, cmdArgs, reqOptions)
	case "readeverything":
		return doReadEverything(client, cmdArgs, reqOptions)
	case "readonlydata":
		return doReadOnlyData(client, cmdArgs, reqOptions)
	case "readdescription":
		return doReadDescription(client, cmdArgs, reqOptions)
	case "readdatastate":
		return doReadDataState(client, cmdArgs, reqOptions)
	case "writedata":
		return doWriteData(client, cmdArgs, reqOptions)
	default:
		fs.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func parseGlobalFlags(args []string) (globalFlags, *pflag.FlagSet, error) {
	var g globalFlags

	fs := pflag.NewFlagSet("gosip", pflag.ContinueOnError)
	fs.SetInterspersed(false)
	fs.SortFlags = false
	fs.StringVarP(&g.address, "address", "a", fmt.Sprintf("127.0.0.1:%d", sip.Port), "[optional] address of the S/IP server")
	fs.IntVarP(&g.busyTimeout, "busytimeout", "b", 0, "[optional] busy timeout in ms (default: server default)")
	fs.IntVarP(&g.leaseTimeout, "leasetimeout", "l", 0, "[optional] lease timeout in ms (default: server default)")
	fs.BoolVarP(&g.keepAlive, "keepalive", "k", false, "[optional] send keep alive pings (default: false)")
	fs.DurationVarP(&g.dialTimeout, "dialtimeout", "d", 5*time.Second, "[optional] timeout for the initial connection")
	fs.DurationVarP(&g.timeout, "timeout", "t", 5*time.Second, "[optional] timeout per request (0: no timeout)")
	fs.UintVarP(&g.retries, "retries", "r", 0, "[optional] number of retries per request (default: 0)")
	fs.BoolVarP(&g.verbose, "verbose", "v", false, "[optional] enable S/IP logging on stderr (default: false)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: gosip [global flags] <command> [command flags]\n\nglobal flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), `
commands:
  ping             send a S/IP Ping
  readeverything   read description, data state and data of an IDN
  readonlydata     read only the data of an IDN
  readdescription  read only the description of an IDN
  readdatastate    read only the data state of an IDN
  writedata        write data to an IDN
  browse           broadcast a Browse request and list the answering devices
  identify         broadcast an Identify request to a device
  setip            broadcast a SetIP request to configure a device

The broadcast commands browse, identify and setip use UDP and ignore the
connection related global flags.

Run "gosip <command> -h" for the flags of a command.
`)
	}
	err := fs.Parse(args)
	return g, fs, err
}

// target holds the flags shared by all IDN addressed commands.
type target struct {
	slaveIndex     int
	slaveExtension int
	idn            uint32
}

func parseTarget(name string, args []string, extra func(*pflag.FlagSet)) (target, *pflag.FlagSet, error) {
	var t target
	var idnStr string

	fs := newFlagSet(name)
	fs.StringVarP(&idnStr, "idn", "i", "", `[mandatory] IDN, either numeric (4106, 0x100A) or Sercos notation (S-0-0095.0.0)`)
	if extra != nil {
		extra(fs)
	}
	fs.IntVarP(&t.slaveIndex, "slave", "s", 0, "[optional] slave index (default: 0)")
	fs.IntVarP(&t.slaveExtension, "ext", "e", 0, "[optional] slave extension (default: 0)")
	if err := fs.Parse(args); err != nil {
		return t, fs, err
	}
	if idnStr == "" {
		fs.Usage()
		return t, fs, errors.New("--idn is required")
	}
	idn, err := parseIdn(idnStr)
	if err != nil {
		return t, fs, err
	}
	t.idn = idn
	return t, fs, nil
}

func doPing(client sip.Client, args []string, options []sip.RequestOption) error {
	fs := newFlagSet("ping")
	if err := fs.Parse(args); err != nil {
		return err
	}
	start := time.Now()
	if err := client.Ping(options...); err != nil {
		return err
	}
	fmt.Printf("ping ok (%v)\n", time.Since(start).Round(time.Microsecond))
	return nil
}

func doReadEverything(client sip.Client, args []string, options []sip.RequestOption) error {
	t, _, err := parseTarget("readeverything", args, nil)
	if err != nil {
		return err
	}
	resp, err := client.ReadEverything(t.slaveIndex, t.slaveExtension, t.idn, options...)
	if err != nil {
		return err
	}
	fmt.Printf("IDN:           %s\n", sip.Idn(t.idn))
	fmt.Printf("ValidElements: 0x%04x (%s)\n", resp.ValidElements, validElements(resp.ValidElements))
	fmt.Printf("DataState:     0x%04x\n", resp.DataState)
	fmt.Printf("Attribute:     0x%08x\n", resp.Attribute)
	fmt.Printf("Name:          %s\n", string(resp.Name))
	fmt.Printf("Unit:          %s\n", string(resp.Unit))
	fmt.Printf("Min:           %s\n", hex.EncodeToString(resp.Min[:]))
	fmt.Printf("Max:           %s\n", hex.EncodeToString(resp.Max[:]))
	fmt.Printf("MaxListLength: %d\n", resp.MaxListLength)
	printData(resp.Data)
	return nil
}

func doReadOnlyData(client sip.Client, args []string, options []sip.RequestOption) error {
	t, _, err := parseTarget("readonlydata", args, nil)
	if err != nil {
		return err
	}
	resp, err := client.ReadOnlyData(t.slaveIndex, t.slaveExtension, t.idn, options...)
	if err != nil {
		return err
	}
	fmt.Printf("IDN:       %s\n", sip.Idn(t.idn))
	fmt.Printf("Attribute: 0x%08x\n", resp.Attribute)
	printData(resp.Data)
	return nil
}

func doReadDescription(client sip.Client, args []string, options []sip.RequestOption) error {
	t, _, err := parseTarget("readdescription", args, nil)
	if err != nil {
		return err
	}
	resp, err := client.ReadDescription(t.slaveIndex, t.slaveExtension, t.idn, options...)
	if err != nil {
		return err
	}
	fmt.Printf("IDN:           %s\n", sip.Idn(t.idn))
	fmt.Printf("ValidElements: 0x%04x (%s)\n", resp.ValidElements, validElements(resp.ValidElements))
	fmt.Printf("Attribute:     0x%08x\n", resp.Attribute)
	fmt.Printf("Name:          %s\n", string(resp.Name))
	fmt.Printf("Unit:          %s\n", string(resp.Unit))
	fmt.Printf("Min:           %s\n", hex.EncodeToString(resp.Min[:]))
	fmt.Printf("Max:           %s\n", hex.EncodeToString(resp.Max[:]))
	fmt.Printf("MaxListLength: %d\n", resp.MaxListLength)
	return nil
}

func doReadDataState(client sip.Client, args []string, options []sip.RequestOption) error {
	t, _, err := parseTarget("readdatastate", args, nil)
	if err != nil {
		return err
	}
	resp, err := client.ReadDataState(t.slaveIndex, t.slaveExtension, t.idn, options...)
	if err != nil {
		return err
	}
	fmt.Printf("IDN:       %s\n", sip.Idn(t.idn))
	fmt.Printf("DataState: 0x%04x\n", resp.DataState)
	return nil
}

func doWriteData(client sip.Client, args []string, options []sip.RequestOption) error {
	var dataStr string
	t, fs, err := parseTarget("writedata", args, func(fs *pflag.FlagSet) {
		fs.StringVarP(&dataStr, "data", "d", "", "[mandatory] data as hex string, e.g. 0a1b2c3d")
	})
	if err != nil {
		return err
	}
	if dataStr == "" {
		fs.Usage()
		return errors.New("--data is required")
	}
	data, err := hex.DecodeString(strings.TrimPrefix(strings.ReplaceAll(dataStr, " ", ""), "0x"))
	if err != nil {
		return fmt.Errorf("invalid --data: %w", err)
	}
	if err := client.WriteData(t.slaveIndex, t.slaveExtension, t.idn, data, options...); err != nil {
		return err
	}
	fmt.Printf("wrote %d bytes to %s\n", len(data), sip.Idn(t.idn))
	return nil
}

func doBrowse(args []string) error {
	var interfaceName string
	var listen time.Duration

	fs := newFlagSet("browse")
	fs.StringVarP(&interfaceName, "interface", "i", "", "[mandatory] name of the network interface to broadcast on, e.g. en0")
	fs.DurationVarP(&listen, "listen", "l", 3*time.Second, "[optional] how long to listen for responses")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if interfaceName == "" && fs.NArg() == 1 {
		interfaceName = fs.Arg(0)
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return errors.New("only one interface name is allowed")
	}
	if interfaceName == "" {
		fs.Usage()
		return errors.New("--interface is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), listen)
	defer cancel()

	results, err := sip.Browse(ctx, interfaceName)
	if err != nil {
		return err
	}
	count := 0
	for result := range results {
		if result.Err != nil {
			fmt.Fprintln(os.Stderr, "error:", result.Err)
			continue
		}
		if result.Ok == nil {
			fmt.Fprintln(os.Stderr, "error: browse returned an empty response")
			continue
		}
		count++
		b := result.Ok
		fmt.Printf("device %d\n", count)
		fmt.Printf("  DisplayName:    %s\n", string(b.DisplayName))
		fmt.Printf("  HostName:       %s\n", string(b.HostName))
		fmt.Printf("  NodeIdentifier: %s\n", formatMAC(b.NodeIdentifier))
		fmt.Printf("  MacAddress:     %s\n", formatMAC(b.MacAddress))
		fmt.Printf("  IPAddress:      %s\n", net.IP(b.IPAddress[:]))
		fmt.Printf("  Subnet:         %s\n", net.IP(b.Subnet[:]))
		fmt.Printf("  Gateway:        %s\n", net.IP(b.Gateway[:]))
		fmt.Printf("  DHCPMode:       %d\n", b.DHCPMode)
		fmt.Printf("  DHCPFeatures:   0x%02x\n", b.DHCPFeatures)
		fmt.Printf("  Version:        %d\n", b.Version)
	}
	fmt.Printf("%d device(s) found\n", count)
	return nil
}

func doIdentify(args []string) error {
	var interfaceName, nodeStr string
	var listen time.Duration

	fs := newFlagSet("identify")
	fs.StringVarP(&interfaceName, "interface", "i", "", "[mandatory] name of the network interface to broadcast on, e.g. en0")
	fs.StringVarP(&nodeStr, "node", "n", "", "[mandatory] node identifier of the device, e.g. 00:11:22:33:44:55")
	fs.DurationVarP(&listen, "listen", "l", 3*time.Second, "[optional] how long to listen for responses")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if interfaceName == "" || nodeStr == "" {
		fs.Usage()
		return errors.New("--interface and --node are required")
	}
	node, err := parseNodeIdentifier(nodeStr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listen)
	defer cancel()

	results, err := sip.Identify(ctx, interfaceName, node)
	if err != nil {
		return err
	}
	return countResponses(results, "identify")
}

func doSetIP(args []string) error {
	var interfaceName, nodeStr, ipStr, gatewayStr string
	var persist bool
	var listen time.Duration

	fs := newFlagSet("setip")
	fs.StringVarP(&interfaceName, "interface", "i", "", "[mandatory] name of the network interface to broadcast on, e.g. en0")
	fs.StringVarP(&nodeStr, "node", "n", "", "[mandatory] node identifier of the device, e.g. 00:11:22:33:44:55")
	fs.StringVarP(&ipStr, "ip", "p", "", "[mandatory] IPv4 address to set, e.g. 192.168.1.100")
	fs.StringVarP(&gatewayStr, "gateway", "g", "0.0.0.0", "[optional] IPv4 gateway address")
	fs.BoolVarP(&persist, "persist", "P", false, "[optional] store the address persistently (default: false)")
	fs.DurationVarP(&listen, "listen", "l", 3*time.Second, "[optional] how long to listen for responses")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if interfaceName == "" || nodeStr == "" || ipStr == "" {
		fs.Usage()
		return errors.New("--interface, --node and --ip are required")
	}
	node, err := parseNodeIdentifier(nodeStr)
	if err != nil {
		return err
	}
	ip, err := parseIPv4(ipStr)
	if err != nil {
		return fmt.Errorf("invalid --ip: %w", err)
	}
	gateway, err := parseIPv4(gatewayStr)
	if err != nil {
		return fmt.Errorf("invalid --gateway: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), listen)
	defer cancel()

	results, err := sip.SetIP(ctx, interfaceName, node, ip, gateway, persist)
	if err != nil {
		return err
	}
	return countResponses(results, "setip")
}

// countResponses drains the result channel of a broadcast which has an empty
// response PDU and reports how many devices answered.
func countResponses[T any](results chan sip.Result[T], name string) error {
	count := 0
	for result := range results {
		if result.Err != nil {
			fmt.Fprintln(os.Stderr, "error:", result.Err)
			continue
		}
		count++
	}
	fmt.Printf("%s: %d device(s) responded\n", name, count)
	return nil
}

func parseNodeIdentifier(s string) ([6]byte, error) {
	var node [6]byte
	cleaned := strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(s)
	b, err := hex.DecodeString(cleaned)
	if err != nil || len(b) != 6 {
		return node, fmt.Errorf("invalid node identifier %q: expected 6 bytes, e.g. 00:11:22:33:44:55", s)
	}
	return [6]byte(b), nil
}

func parseIPv4(s string) (net.IP, error) {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("%q is not an IPv4 address", s)
	}
	return ip.To4(), nil
}

func formatMAC(b [6]byte) string {
	return net.HardwareAddr(b[:]).String()
}

// parseIdn accepts a decimal or hexadecimal number or the Sercos notation
// S-0-0095, P-0-0100.1.2 as printed by sip.Idn.String.
func parseIdn(s string) (uint32, error) { // nolint:cyclop
	s = strings.TrimSpace(s)
	if u, err := strconv.ParseUint(s, 0, 32); err == nil {
		return uint32(u), nil // nolint:gosec
	}

	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid IDN %q", s)
	}
	var idn uint32
	switch strings.ToUpper(parts[0]) {
	case "S":
	case "P":
		idn |= 0x8000
	default:
		return 0, fmt.Errorf("invalid IDN %q: type must be S or P", s)
	}
	set, err := strconv.ParseUint(parts[1], 10, 8)
	if err != nil || set > 7 {
		return 0, fmt.Errorf("invalid IDN %q: parameter set must be 0..7", s)
	}
	idn |= uint32(set) << 12

	// block number, optionally followed by .structureInstance.structureElement
	nums := strings.Split(parts[2], ".")
	if len(nums) != 1 && len(nums) != 3 {
		return 0, fmt.Errorf("invalid IDN %q", s)
	}
	block, err := strconv.ParseUint(nums[0], 10, 16)
	if err != nil || block > 0x0fff {
		return 0, fmt.Errorf("invalid IDN %q: block number must be 0..4095", s)
	}
	idn |= uint32(block)
	if len(nums) == 3 {
		si, err := strconv.ParseUint(nums[1], 10, 8)
		if err != nil {
			return 0, fmt.Errorf("invalid IDN %q: invalid structure instance", s)
		}
		se, err := strconv.ParseUint(nums[2], 10, 8)
		if err != nil {
			return 0, fmt.Errorf("invalid IDN %q: invalid structure element", s)
		}
		idn |= uint32(si)<<24 | uint32(se)<<16
	}
	return idn, nil
}

func validElements(ve uint16) string {
	names := []struct {
		mask uint16
		name string
	}{
		{sip.ElmDataState, "DataState"},
		{sip.ElmName, "Name"},
		{sip.ElmAttribute, "Attribute"},
		{sip.ElmUnit, "Unit"},
		{sip.ElmMin, "Min"},
		{sip.ElmMax, "Max"},
		{sip.ElmData, "Data"},
	}
	set := []string{}
	for _, n := range names {
		if ve&n.mask == n.mask {
			set = append(set, n.name)
		}
	}
	if len(set) == 0 {
		return "none"
	}
	return strings.Join(set, "|")
}

func printData(data []byte) {
	fmt.Printf("DataLength:    %d\n", len(data))
	if len(data) == 0 {
		return
	}
	fmt.Printf("Data (hex):    %s\n", hex.EncodeToString(data))
	fmt.Printf("Data (text):   %s\n", strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return '.'
		}
		return r
	}, string(data)))
}
