package dialer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/daminit/traffics-cli/infra/constant"
	"github.com/daminit/traffics-cli/infra/meta"
	"github.com/daminit/traffics-cli/infra/networks/resolve"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/metadata"
	"net"
	"net/netip"
	"runtime"
	"strconv"
	"time"
)

type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type DialConfig struct {
	Resolver Resolver
	Strategy meta.Strategy

	Timeout      time.Duration
	Interface    string
	BindAddress4 netip.Addr
	BindAddress6 netip.Addr
	FwMark       uint32
	ReuseAddr    bool
	// tcp
	MPTCP bool

	// udp
	UDPFragment bool
}

func NewDefault(config DialConfig) (*DefaultDialer, error) {
	var (
		dialer   net.Dialer
		listener net.ListenConfig
	)
	if config.Interface != "" {
		finder := control.NewDefaultInterfaceFinder()
		bindFunc := control.BindToInterface(finder, config.Interface, -1)
		dialer.Control = control.Append(dialer.Control, bindFunc)
		listener.Control = control.Append(listener.Control, bindFunc)
	}
	if config.Resolver == nil {
		config.Resolver = DefaultResolver
	}
	if config.FwMark != 0 {
		if runtime.GOOS != "linux" {
			return nil, errors.New("`fwmark` is only supported on Linux")
		}
		dialer.Control = control.Append(dialer.Control, control.RoutingMark(config.FwMark))
		listener.Control = control.Append(listener.Control, control.RoutingMark(config.FwMark))
	}
	dialer.Timeout = cmp.Or(config.Timeout, constant.DefaultDialerTimeout)

	// TODO: customize keepAlive(dialer)
	dialer.KeepAliveConfig = net.KeepAliveConfig{
		Enable:   true,
		Idle:     constant.DefaultTCPKeepAliveInitial,
		Interval: constant.DefaultTCPKeepAliveInterval,
		Count:    constant.DefaultTCPKeepAliveProbeCount,
	}
	if config.ReuseAddr {
		listener.Control = control.Append(listener.Control, control.ReuseAddr())
	}

	if !config.UDPFragment {
		dialer.Control = control.Append(dialer.Control, control.DisableUDPFragment())
		listener.Control = control.Append(listener.Control, control.DisableUDPFragment())
	}
	if config.MPTCP {
		dialer.SetMultipathTCP(true)
	}

	var (
		dialer4 net.Dialer
		dialer6 net.Dialer

		udpDialer4 net.Dialer
		udpDialer6 net.Dialer

		udpAddr4 string
		udpAddr6 string
	)

	if config.BindAddress4.IsValid() {
		bind := config.BindAddress4
		dialer4.LocalAddr = &net.TCPAddr{IP: bind.AsSlice()}
		udpDialer4.LocalAddr = &net.UDPAddr{IP: bind.AsSlice()}
		udpAddr4 = bind.String()
	}

	if config.BindAddress6.IsValid() {
		bind := config.BindAddress6
		dialer6.LocalAddr = &net.TCPAddr{IP: bind.AsSlice()}
		udpDialer6.LocalAddr = &net.UDPAddr{IP: bind.AsSlice()}
		udpAddr6 = bind.String()
	}

	return &DefaultDialer{
		defaultDialer:   dialer,
		dialer4:         dialer4,
		dialer6:         dialer6,
		udpDialer4:      udpDialer4,
		udpDialer6:      udpDialer6,
		udpAddr4:        udpAddr4,
		udpAddr6:        udpAddr6,
		resolver:        config.Resolver,
		resolveStrategy: config.Strategy,
	}, nil
}

type DefaultDialer struct {
	defaultDialer net.Dialer

	dialer4 net.Dialer
	dialer6 net.Dialer

	udpDialer4 net.Dialer
	udpDialer6 net.Dialer

	udpAddr4 string
	udpAddr6 string

	resolver        resolve.Resolver
	resolveStrategy meta.Strategy
}

func (d *DefaultDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	switch network {
	case "udp", "udp4", "udp6", "tcp", "tcp4", "tcp6":
	default:
		// fallback to default dialer
		return d.defaultDialer.DialContext(ctx, network, address)
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("dialer: split host port failed: %s: %w", address, err)
	}
	portNum, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("dialer: invalid port number: %s: %w", port, err)
	}
	if !metadata.IsDomainName(host) {
		addr, err := netip.ParseAddr(host)
		if err != nil {
			return nil, fmt.Errorf("dialer: invalid address: %s: %w", host, err)
		}
		return d.DialSerial(ctx, network, []netip.Addr{addr}, uint16(portNum))
	}
	a, aaaa, err := d.resolver.Lookup(ctx, host, d.resolveStrategy)
	if err != nil {
		return nil, fmt.Errorf("dialer: resolve address for %s failed: %w", address, err)
	}

	return d.DialParallel(ctx, network, d.resolveStrategy, a, aaaa, uint16(portNum))
}

func (d *DefaultDialer) DialSerial(ctx context.Context, network string, addresses []netip.Addr, port uint16) (net.Conn, error) {
	conn, err := d.dialSerial(ctx, network, addresses, port)
	if err != nil {
		return nil, fmt.Errorf("dialer: %w", err)
	}
	return conn, nil
}

func (d *DefaultDialer) dialSerial(ctx context.Context, network string, addresses []netip.Addr, port uint16) (net.Conn, error) {
	if len(addresses) == 0 {
		return nil, errors.New("no address to dial")
	}
	var (
		nn meta.Network
		ok bool
	)

	if nn, ok = meta.ParseNetwork(network); !ok {
		return nil, fmt.Errorf("invalid network :%s", network)
	}

	availableAddress := filterAddressByNetwork(nn, addresses)
	if len(availableAddress) == 0 {
		return nil, fmt.Errorf("no available address found for network: %s", network)
	}

	var lastErr error
	for _, addr := range availableAddress {
		if common.Done(ctx) {
			return nil, ctx.Err()
		}
		var (
			target    = netip.AddrPortFrom(addr, port)
			conn      net.Conn
			err       error
			tcpDialer *net.Dialer
			udpDialer *net.Dialer
		)
		switch {
		case addr.Is4():
			udpDialer = &d.udpDialer4
			tcpDialer = &d.dialer4
		case addr.Is6():
			udpDialer = &d.udpDialer4
			tcpDialer = &d.dialer4
		default:
			tcpDialer = &d.defaultDialer
			udpDialer = &d.defaultDialer
		}
		switch nn.Protocol {
		case meta.ProtocolUDP:
			conn, err = udpDialer.DialContext(ctx, network, target.String())
		case meta.ProtocolTCP:
			conn, err = tcpDialer.DialContext(ctx, network, target.String())
		default:
			conn, err = d.defaultDialer.DialContext(ctx, network, addr.String())
		}

		if err == nil {
			return conn, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("all addresses failed, last error: %w", lastErr)
}

func (d *DefaultDialer) DialParallel(ctx context.Context, network string, strategy meta.Strategy,
	ipv4 []netip.Addr, ipv6 []netip.Addr, port uint16) (net.Conn, error) {
	if len(ipv4) == 0 && len(ipv6) == 0 {
		return nil, fmt.Errorf("dialer: no available address to dial")
	}

	if len(ipv4) == 0 || strategy == meta.StrategyIPv6Only {
		return d.DialSerial(ctx, network, ipv6, port)
	}
	if len(ipv6) == 0 || strategy == meta.StrategyIPv4Only {
		return d.DialSerial(ctx, network, ipv4, port)
	}

	// happy eyeball implement
	type dialResult struct {
		conn net.Conn
		err  error
		ipv6 bool
	}

	resultChan := make(chan dialResult, 2)
	dialCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	firstIsIPv4 := strategy == meta.StrategyPreferIPv4
	var first, second []netip.Addr
	if firstIsIPv4 {
		first, second = ipv4, ipv6
	} else {
		first, second = ipv6, ipv4
	}

	go func() {
		conn, err := d.DialSerial(dialCtx, network, first, port)
		select {
		case resultChan <- dialResult{conn: conn, err: err,
			ipv6: !firstIsIPv4}:
		case <-dialCtx.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// happy eyeball
	firstTimer := time.NewTimer(300 * time.Millisecond)
	defer firstTimer.Stop()

	var secondStarted bool
	var resultsReceived int

	for resultsReceived < 2 {
		select {
		case <-dialCtx.Done():
			return nil, dialCtx.Err()

		case <-firstTimer.C:
			if !secondStarted {
				secondStarted = true
				go func() {
					conn, err := d.DialSerial(dialCtx, network, second, port)
					select {
					case resultChan <- dialResult{conn: conn, err: err, ipv6: firstIsIPv4}:
					case <-dialCtx.Done():
						if conn != nil {
							conn.Close()
						}
					}
				}()
			}

		case result := <-resultChan:
			resultsReceived++

			if result.err == nil {
				cancel()
				return result.conn, nil
			}

			if !secondStarted && resultsReceived == 1 {
				secondStarted = true
				firstTimer.Stop()
				go func() {
					conn, err := d.DialSerial(dialCtx, network, second, port)
					select {
					case resultChan <- dialResult{conn: conn, err: err, ipv6: firstIsIPv4}:
					case <-dialCtx.Done():
						if conn != nil {
							conn.Close()
						}
					}
				}()
			}
		}
	}

	return nil, fmt.Errorf("dialer: all parallel dials failed for both IPv4 and IPv6")
}

func filterAddressByNetwork(network meta.Network, addr []netip.Addr) []netip.Addr {
	switch {
	case network.Version == meta.NetworkVersionDual:
		return common.Filter(addr, func(it netip.Addr) bool {
			return it.IsValid()
		})
	case network.Version == meta.NetworkVersion4:
		return common.Filter(addr, func(it netip.Addr) bool {
			return it.IsValid() && it.Is4()
		})
	case network.Version == meta.NetworkVersion6:
		return common.Filter(addr, func(it netip.Addr) bool {
			return it.IsValid() && it.Is6()
		})
	default:
		return addr
	}
}
