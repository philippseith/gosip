package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/philippseith/gosip/sercos"
	"github.com/philippseith/gosip/sip"
)

const mcpInstructions = "S/IP (Sercos Internet Protocol) client. IDNs are given as numbers (4106, 0x100A) or Sercos notation (S-0-0095, P-0-0100.1.2). Every device tool requires the address parameter (host[:port], default port 35021); tools whose names are browse, identify and setip use UDP broadcasts on a network interface. Browse without an interface lists the available interfaces."

// mcpClient is the part of sip.Client used by the MCP server.
type mcpClient interface {
	sip.SyncClient
	LeaseTimeout() time.Duration
	Closed() bool
	Close() error
}

type cacheEntry struct {
	client   mcpClient
	inUse    int
	lastUsed time.Time
	timer    *time.Timer
}

type clientCache struct {
	mu           sync.Mutex
	defaultLease time.Duration
	dial         func(address string) (mcpClient, error)
	now          func() time.Time
	clients      map[string]*cacheEntry
}

func newClientCache(defaultLease time.Duration, dial func(address string) (mcpClient, error)) *clientCache {
	return &clientCache{
		defaultLease: defaultLease,
		dial:         dial,
		now:          time.Now,
		clients:      make(map[string]*cacheEntry),
	}
}

func (c *clientCache) get(address string) (mcpClient, func(), error) {
	if err := required("address", address); err != nil {
		return nil, nil, err
	}
	key := addressWithDefaultPort(address)

	c.mu.Lock()
	expired := c.evictIdleLocked()
	entry := c.clients[key]
	if entry != nil && entry.client.Closed() {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		delete(c.clients, key)
		entry = nil
	}
	if entry == nil {
		client, err := c.dial(key)
		if err != nil {
			c.mu.Unlock()
			closeIdleClients(expired)
			return nil, nil, err
		}
		entry = &cacheEntry{client: client}
		c.clients[key] = entry
	}
	entry.inUse++
	c.mu.Unlock()
	closeIdleClients(expired)

	var once sync.Once
	release := func() {
		once.Do(func() {
			c.mu.Lock()
			entry.inUse--
			entry.lastUsed = c.now()
			if c.clients[key] == entry {
				d := c.idleTimeout(entry.client)
				if entry.timer == nil {
					entry.timer = time.AfterFunc(d, c.evictIdle)
				} else {
					entry.timer.Reset(d)
				}
			}
			c.mu.Unlock()
		})
	}
	return entry.client, release, nil
}

func (c *clientCache) idleTimeout(client mcpClient) time.Duration {
	lease := client.LeaseTimeout()
	if lease <= 0 {
		lease = c.defaultLease
	}
	return 2 * lease
}

func (c *clientCache) evictIdle() {
	c.mu.Lock()
	expired := c.evictIdleLocked()
	c.mu.Unlock()
	closeIdleClients(expired)
}

func (c *clientCache) evictIdleLocked() []mcpClient {
	var expired []mcpClient
	for address, entry := range c.clients {
		if entry.inUse != 0 || c.now().Sub(entry.lastUsed) < c.idleTimeout(entry.client) {
			continue
		}
		if entry.timer != nil {
			entry.timer.Stop()
		}
		delete(c.clients, address)
		expired = append(expired, entry.client)
	}
	return expired
}

func closeIdleClients(clients []mcpClient) {
	for _, client := range clients {
		_ = client.Close()
	}
}

func (c *clientCache) Close() error {
	c.mu.Lock()
	clients := make([]mcpClient, 0, len(c.clients))
	for _, entry := range c.clients {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		clients = append(clients, entry.client)
	}
	clear(c.clients)
	c.mu.Unlock()

	var errs []error
	for _, client := range clients {
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type attributeInfo struct {
	Value            uint32 `json:"value"`
	Format           string `json:"format"`
	Length           int    `json:"length"`
	List             bool   `json:"list"`
	DecimalPlaces    int    `json:"decimalPlaces"`
	ConversionFactor int    `json:"conversionFactor"`
}

type limitInfo struct {
	Value string `json:"value"`
	Raw   string `json:"raw"`
}

type dataInfo struct {
	Elements []string `json:"elements"`
	Raw      string   `json:"raw"`
}

var formatNames = [...]string{"bin", "unsigned", "signed", "hex", "text", "idn", "float", "time"}

func newAttributeInfo(a sercos.Attribute) attributeInfo {
	format := "unknown"
	if index := int(a.Format()); index >= 0 && index < len(formatNames) {
		format = formatNames[index]
	}
	return attributeInfo{
		Value:            uint32(a),
		Format:           format,
		Length:           a.Length(),
		List:             a.IsList(),
		DecimalPlaces:    a.DecimalPlaces(),
		ConversionFactor: a.ConversionFactor(),
	}
}

func newLimitInfo(raw [8]byte, a sercos.Attribute) *limitInfo {
	return &limitInfo{Value: formatLimit(raw, a), Raw: formatBytes(raw[:])}
}

func newDataInfo(data []byte, a sercos.Attribute) *dataInfo {
	elements := sercos.Data(data).Strings(a)
	if elements == nil {
		elements = []string{}
	}
	return &dataInfo{Elements: elements, Raw: formatBytes(data)}
}

func (t *mcpTools) target(address string, slave, ext int, idnStr string) (mcpClient, func(), uint32, error) {
	if idnStr == "" {
		return nil, nil, 0, errors.New("idn is required")
	}
	if slave < 0 || slave > 65535 {
		return nil, nil, 0, fmt.Errorf("slave must be 0..65535")
	}
	if ext < 0 || ext > 65535 {
		return nil, nil, 0, fmt.Errorf("ext must be 0..65535")
	}
	idn, err := parseIdn(idnStr)
	if err != nil {
		return nil, nil, 0, err
	}
	client, release, err := t.clients.get(address)
	if err != nil {
		return nil, nil, 0, err
	}
	return client, release, idn, nil
}

func required(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func parseListen(value string) (time.Duration, error) {
	if value == "" {
		return 3 * time.Second, nil
	}
	listen, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid listen duration %q: %w", value, err)
	}
	if listen <= 0 {
		return 0, errors.New("listen must be positive")
	}
	return listen, nil
}

type mcpTools struct {
	clients *clientCache
	options []sip.RequestOption
}

type addressInput struct {
	Address string `json:"address" jsonschema:"device address host[:port] (default port 35021), for example 192.168.1.50"`
}
type idnInput struct {
	Address string `json:"address" jsonschema:"device address host[:port] (default port 35021), for example 192.168.1.50"`
	Slave   int    `json:"slave,omitempty" jsonschema:"slave index from 0 to 65535"`
	Ext     int    `json:"ext,omitempty" jsonschema:"slave extension from 0 to 65535"`
	IDN     string `json:"idn,omitempty" jsonschema:"IDN as a number or Sercos notation"`
}

type writeInput struct {
	Address string `json:"address" jsonschema:"device address host[:port] (default port 35021), for example 192.168.1.50"`
	Slave   int    `json:"slave,omitempty" jsonschema:"slave index from 0 to 65535"`
	Ext     int    `json:"ext,omitempty" jsonschema:"slave extension from 0 to 65535"`
	IDN     string `json:"idn,omitempty" jsonschema:"IDN as a number or Sercos notation"`
	Data    string `json:"data,omitempty" jsonschema:"data to write, formatted values or raw hexadecimal bytes"`
	Raw     bool   `json:"raw,omitempty" jsonschema:"interpret data as raw hexadecimal bytes"`
}

type browseInput struct {
	Interface string `json:"interface,omitempty" jsonschema:"network interface for the UDP broadcast; omit it to list the available interfaces instead of browsing"`
	Listen    string `json:"listen,omitempty" jsonschema:"response listening duration, such as 3s; defaults to 3s"`
}

type identifyInput struct {
	Interface string `json:"interface,omitempty" jsonschema:"network interface for the UDP broadcast"`
	Node      string `json:"node,omitempty" jsonschema:"target node identifier, for example 00:11:22:33:44:55"`
	Listen    string `json:"listen,omitempty" jsonschema:"response listening duration, such as 3s; defaults to 3s"`
}

type setIPInput struct {
	Interface string `json:"interface,omitempty" jsonschema:"network interface for the UDP broadcast"`
	Node      string `json:"node,omitempty" jsonschema:"target node identifier, for example 00:11:22:33:44:55"`
	IP        string `json:"ip,omitempty" jsonschema:"IPv4 address to set"`
	Gateway   string `json:"gateway,omitempty" jsonschema:"IPv4 gateway address; defaults to 0.0.0.0"`
	Persist   bool   `json:"persist,omitempty" jsonschema:"store the address persistently"`
	Listen    string `json:"listen,omitempty" jsonschema:"response listening duration, such as 3s; defaults to 3s"`
}

type pingOutput struct {
	Duration string `json:"duration"`
}

type readEverythingOutput struct {
	IDN           string         `json:"idn"`
	ValidElements []string       `json:"validElements"`
	DataState     *uint16        `json:"dataState,omitempty"`
	Attribute     *attributeInfo `json:"attribute,omitempty"`
	Name          *string        `json:"name,omitempty"`
	Unit          *string        `json:"unit,omitempty"`
	Min           *limitInfo     `json:"min,omitempty"`
	Max           *limitInfo     `json:"max,omitempty"`
	MaxListLength uint32         `json:"maxListLength"`
	Data          *dataInfo      `json:"data,omitempty"`
}

type readDescriptionOutput struct {
	IDN           string         `json:"idn"`
	ValidElements []string       `json:"validElements"`
	Attribute     *attributeInfo `json:"attribute,omitempty"`
	Name          *string        `json:"name,omitempty"`
	Unit          *string        `json:"unit,omitempty"`
	Min           *limitInfo     `json:"min,omitempty"`
	Max           *limitInfo     `json:"max,omitempty"`
	MaxListLength uint32         `json:"maxListLength"`
}

type readOnlyDataOutput struct {
	IDN       string        `json:"idn"`
	Attribute attributeInfo `json:"attribute"`
	Data      dataInfo      `json:"data"`
}

type readDataStateOutput struct {
	IDN       string `json:"idn"`
	DataState uint16 `json:"dataState"`
}

type writeOutput struct {
	IDN          string `json:"idn"`
	BytesWritten int    `json:"bytesWritten"`
	Raw          string `json:"raw"`
}

type browseDevice struct {
	DisplayName    string `json:"displayName"`
	HostName       string `json:"hostName"`
	NodeIdentifier string `json:"nodeIdentifier"`
	MacAddress     string `json:"macAddress"`
	IPAddress      string `json:"ipAddress"`
	Subnet         string `json:"subnet"`
	Gateway        string `json:"gateway"`
	DHCPMode       int    `json:"dhcpMode"`
	DHCPFeatures   int    `json:"dhcpFeatures"`
	Version        uint32 `json:"version"`
}

type browseOutput struct {
	Devices    []browseDevice `json:"devices"`
	Errors     []string       `json:"errors"`
	Interfaces []string       `json:"interfaces,omitempty" jsonschema:"available network interfaces; only set when no interface was given"`
}

type broadcastOutput struct {
	Responded int      `json:"responded"`
	Errors    []string `json:"errors"`
}

var readOnlyAnnotations = &mcp.ToolAnnotations{ReadOnlyHint: true}

func newMCPServer(clients *clientCache, options []sip.RequestOption) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "gosip", Version: buildVersion()}, &mcp.ServerOptions{Instructions: mcpInstructions})
	tools := &mcpTools{clients: clients, options: options}
	mcp.AddTool(server, &mcp.Tool{Name: "ping", Description: "Send a S/IP Ping.", Annotations: readOnlyAnnotations}, tools.ping)
	mcp.AddTool(server, &mcp.Tool{Name: "readeverything", Description: "Read description, data state and data of an IDN.", Annotations: readOnlyAnnotations}, tools.readEverything)
	mcp.AddTool(server, &mcp.Tool{Name: "readonlydata", Description: "Read the data and the attribute of an IDN.", Annotations: readOnlyAnnotations}, tools.readOnlyData)
	mcp.AddTool(server, &mcp.Tool{Name: "readdescription", Description: "Read only the description of an IDN.", Annotations: readOnlyAnnotations}, tools.readDescription)
	mcp.AddTool(server, &mcp.Tool{Name: "readdatastate", Description: "Read only the data state of an IDN.", Annotations: readOnlyAnnotations}, tools.readDataState)
	mcp.AddTool(server, &mcp.Tool{Name: "writedata", Description: "Write data to an IDN."}, tools.writeData)
	mcp.AddTool(server, &mcp.Tool{Name: "browse", Description: "Broadcast a Browse request and list the answering devices.", Annotations: readOnlyAnnotations}, tools.browse)
	mcp.AddTool(server, &mcp.Tool{Name: "identify", Description: "Broadcast an Identify request."}, tools.identify)
	mcp.AddTool(server, &mcp.Tool{Name: "setip", Description: "Broadcast a SetIP request."}, tools.setIP)
	return server
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "devel"
}

func (t *mcpTools) requestOptions(ctx context.Context) []sip.RequestOption {
	return append(slices.Clone(t.options), sip.WithContext(ctx))
}

func (t *mcpTools) ping(ctx context.Context, _ *mcp.CallToolRequest, in addressInput) (*mcp.CallToolResult, pingOutput, error) {
	client, release, err := t.clients.get(in.Address)
	if err != nil {
		return nil, pingOutput{}, err
	}
	defer release()
	start := time.Now()
	if err := client.Ping(t.requestOptions(ctx)...); err != nil {
		return nil, pingOutput{}, err
	}
	return nil, pingOutput{Duration: time.Since(start).Round(time.Microsecond).String()}, nil
}

func (t *mcpTools) readEverything(ctx context.Context, _ *mcp.CallToolRequest, in idnInput) (*mcp.CallToolResult, readEverythingOutput, error) {
	client, release, idn, err := t.target(in.Address, in.Slave, in.Ext, in.IDN)
	if err != nil {
		return nil, readEverythingOutput{}, err
	}
	defer release()
	resp, err := client.ReadEverything(in.Slave, in.Ext, idn, t.requestOptions(ctx)...)
	if err != nil {
		return nil, readEverythingOutput{}, err
	}
	attribute, name, unit, minLimit, maxLimit := descriptionValues(resp.ValidElements, resp.Attribute, resp.Name, resp.Unit, resp.Min, resp.Max)
	out := readEverythingOutput{
		IDN:           sercos.Idn(idn).String(),
		ValidElements: validElementNames(resp.ValidElements),
		Attribute:     attribute,
		Name:          name,
		Unit:          unit,
		Min:           minLimit,
		Max:           maxLimit,
		MaxListLength: resp.MaxListLength,
	}
	if resp.ValidElements&sip.ElmDataState != 0 {
		dataState := resp.DataState
		out.DataState = &dataState
	}
	if resp.ValidElements&sip.ElmData != 0 {
		out.Data = newDataInfo(resp.Data, sercos.Attribute(resp.Attribute))
	}
	return nil, out, nil
}

func (t *mcpTools) readOnlyData(ctx context.Context, _ *mcp.CallToolRequest, in idnInput) (*mcp.CallToolResult, readOnlyDataOutput, error) {
	client, release, idn, err := t.target(in.Address, in.Slave, in.Ext, in.IDN)
	if err != nil {
		return nil, readOnlyDataOutput{}, err
	}
	defer release()
	resp, err := client.ReadOnlyData(in.Slave, in.Ext, idn, t.requestOptions(ctx)...)
	if err != nil {
		return nil, readOnlyDataOutput{}, err
	}
	attribute := sercos.Attribute(resp.Attribute)
	return nil, readOnlyDataOutput{
		IDN:       sercos.Idn(idn).String(),
		Attribute: newAttributeInfo(attribute),
		Data:      *newDataInfo(resp.Data, attribute),
	}, nil
}

func (t *mcpTools) readDescription(ctx context.Context, _ *mcp.CallToolRequest, in idnInput) (*mcp.CallToolResult, readDescriptionOutput, error) {
	client, release, idn, err := t.target(in.Address, in.Slave, in.Ext, in.IDN)
	if err != nil {
		return nil, readDescriptionOutput{}, err
	}
	defer release()
	resp, err := client.ReadDescription(in.Slave, in.Ext, idn, t.requestOptions(ctx)...)
	if err != nil {
		return nil, readDescriptionOutput{}, err
	}
	attribute, name, unit, minLimit, maxLimit := descriptionValues(resp.ValidElements, resp.Attribute, resp.Name, resp.Unit, resp.Min, resp.Max)
	return nil, readDescriptionOutput{
		IDN:           sercos.Idn(idn).String(),
		ValidElements: validElementNames(resp.ValidElements),
		Attribute:     attribute,
		Name:          name,
		Unit:          unit,
		Min:           minLimit,
		Max:           maxLimit,
		MaxListLength: resp.MaxListLength,
	}, nil
}

func (t *mcpTools) readDataState(ctx context.Context, _ *mcp.CallToolRequest, in idnInput) (*mcp.CallToolResult, readDataStateOutput, error) {
	client, release, idn, err := t.target(in.Address, in.Slave, in.Ext, in.IDN)
	if err != nil {
		return nil, readDataStateOutput{}, err
	}
	defer release()
	resp, err := client.ReadDataState(in.Slave, in.Ext, idn, t.requestOptions(ctx)...)
	if err != nil {
		return nil, readDataStateOutput{}, err
	}
	return nil, readDataStateOutput{IDN: sercos.Idn(idn).String(), DataState: resp.DataState}, nil
}

func (t *mcpTools) writeData(ctx context.Context, _ *mcp.CallToolRequest, in writeInput) (*mcp.CallToolResult, writeOutput, error) {
	if err := required("data", in.Data); err != nil {
		return nil, writeOutput{}, err
	}
	client, release, idn, err := t.target(in.Address, in.Slave, in.Ext, in.IDN)
	if err != nil {
		return nil, writeOutput{}, err
	}
	defer release()
	options := t.requestOptions(ctx)
	data, err := encodeWrite(client, in.Slave, in.Ext, idn, in.Data, in.Raw, options)
	if err != nil {
		return nil, writeOutput{}, err
	}
	if err := client.WriteData(in.Slave, in.Ext, idn, data, options...); err != nil {
		return nil, writeOutput{}, err
	}
	return nil, writeOutput{IDN: sercos.Idn(idn).String(), BytesWritten: len(data), Raw: formatBytes(data)}, nil
}

func (t *mcpTools) browse(ctx context.Context, _ *mcp.CallToolRequest, in browseInput) (*mcp.CallToolResult, browseOutput, error) {
	if strings.TrimSpace(in.Interface) == "" {
		names, err := availableInterfaces()
		if err != nil {
			return nil, browseOutput{}, err
		}
		if len(names) == 0 {
			return nil, browseOutput{}, errors.New("no network interface with an IPv4 address is up")
		}
		return nil, browseOutput{Devices: []browseDevice{}, Errors: []string{}, Interfaces: names}, nil
	}
	listen, err := parseListen(in.Listen)
	if err != nil {
		return nil, browseOutput{}, err
	}
	listenCtx, cancel := context.WithTimeout(ctx, listen)
	defer cancel()
	results, err := sip.Browse(listenCtx, in.Interface)
	if err != nil {
		return nil, browseOutput{}, err
	}
	return nil, collectBrowseResults(results), nil
}

func collectBrowseResults(results <-chan sip.Result[*sip.BrowseResponse]) browseOutput {
	out := browseOutput{Devices: []browseDevice{}, Errors: []string{}}
	for result := range results {
		if result.Err != nil {
			out.Errors = append(out.Errors, result.Err.Error())
			continue
		}
		if result.Ok == nil {
			out.Errors = append(out.Errors, "browse returned an empty response")
			continue
		}
		out.Devices = append(out.Devices, newBrowseDevice(result.Ok))
	}
	return out
}

func newBrowseDevice(resp *sip.BrowseResponse) browseDevice {
	return browseDevice{
		DisplayName:    string(resp.DisplayName),
		HostName:       string(resp.HostName),
		NodeIdentifier: formatMAC(resp.NodeIdentifier),
		MacAddress:     formatMAC(resp.MacAddress),
		IPAddress:      net.IP(resp.IPAddress[:]).String(),
		Subnet:         net.IP(resp.Subnet[:]).String(),
		Gateway:        net.IP(resp.Gateway[:]).String(),
		DHCPMode:       int(resp.DHCPMode),
		DHCPFeatures:   int(resp.DHCPFeatures),
		Version:        resp.Version,
	}
}

func (t *mcpTools) identify(ctx context.Context, _ *mcp.CallToolRequest, in identifyInput) (*mcp.CallToolResult, broadcastOutput, error) {
	if err := required("interface", in.Interface); err != nil {
		return nil, broadcastOutput{}, err
	}
	if err := required("node", in.Node); err != nil {
		return nil, broadcastOutput{}, err
	}
	node, err := parseNodeIdentifier(in.Node)
	if err != nil {
		return nil, broadcastOutput{}, err
	}
	listen, err := parseListen(in.Listen)
	if err != nil {
		return nil, broadcastOutput{}, err
	}
	listenCtx, cancel := context.WithTimeout(ctx, listen)
	defer cancel()
	results, err := sip.Identify(listenCtx, in.Interface, node)
	if err != nil {
		return nil, broadcastOutput{}, err
	}
	return nil, collectBroadcastResults(results), nil
}

func (t *mcpTools) setIP(ctx context.Context, _ *mcp.CallToolRequest, in setIPInput) (*mcp.CallToolResult, broadcastOutput, error) {
	if err := required("interface", in.Interface); err != nil {
		return nil, broadcastOutput{}, err
	}
	if err := required("node", in.Node); err != nil {
		return nil, broadcastOutput{}, err
	}
	if err := required("ip", in.IP); err != nil {
		return nil, broadcastOutput{}, err
	}
	node, err := parseNodeIdentifier(in.Node)
	if err != nil {
		return nil, broadcastOutput{}, err
	}
	ip, err := parseIPv4(in.IP)
	if err != nil {
		return nil, broadcastOutput{}, fmt.Errorf("invalid ip: %w", err)
	}
	gatewayValue := in.Gateway
	if gatewayValue == "" {
		gatewayValue = "0.0.0.0"
	}
	gateway, err := parseIPv4(gatewayValue)
	if err != nil {
		return nil, broadcastOutput{}, fmt.Errorf("invalid gateway: %w", err)
	}
	listen, err := parseListen(in.Listen)
	if err != nil {
		return nil, broadcastOutput{}, err
	}
	listenCtx, cancel := context.WithTimeout(ctx, listen)
	defer cancel()
	results, err := sip.SetIP(listenCtx, in.Interface, node, ip, gateway, in.Persist)
	if err != nil {
		return nil, broadcastOutput{}, err
	}
	return nil, collectBroadcastResults(results), nil
}

func collectBroadcastResults[T any](results <-chan sip.Result[T]) broadcastOutput {
	out := broadcastOutput{Errors: []string{}}
	for result := range results {
		if result.Err != nil {
			out.Errors = append(out.Errors, result.Err.Error())
			continue
		}
		out.Responded++
	}
	return out
}

func descriptionValues(valid uint16, attribute uint32, name, unit []byte, minValue, maxValue [8]byte) (*attributeInfo, *string, *string, *limitInfo, *limitInfo) {
	attr := sercos.Attribute(attribute)
	var attributeResult *attributeInfo
	if valid&sip.ElmAttribute != 0 {
		info := newAttributeInfo(attr)
		attributeResult = &info
	}
	var nameResult, unitResult *string
	if valid&sip.ElmName != 0 {
		value := string(name)
		nameResult = &value
	}
	if valid&sip.ElmUnit != 0 {
		value := string(unit)
		unitResult = &value
	}
	var minResult, maxResult *limitInfo
	if valid&sip.ElmMin != 0 {
		minResult = newLimitInfo(minValue, attr)
	}
	if valid&sip.ElmMax != 0 {
		maxResult = newLimitInfo(maxValue, attr)
	}
	return attributeResult, nameResult, unitResult, minResult, maxResult
}

func doMCP(args []string, dial func(string) (sip.Client, error), defaultLease time.Duration, options []sip.RequestOption) error {
	fs := newFlagSet("mcp")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("mcp takes no arguments")
	}
	wrappedDial := func(address string) (mcpClient, error) {
		client, err := dial(address)
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	clients := newClientCache(defaultLease, wrappedDial)
	defer func() { _ = clients.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := newMCPServer(clients, options).Run(ctx, &mcp.StdioTransport{})
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
