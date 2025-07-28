package listener

import (
	"context"
	"fmt"
	"github.com/daminit/traffics-cli/infra/constant"
	"github.com/daminit/traffics-cli/infra/meta"
	"github.com/metacubex/tfo-go"
	"github.com/sagernet/sing/common/control"
	"net"
	"strconv"
)

type Options struct {
	// optional
	Family    string
	Interface string
	ReuseAddr bool

	// tcp
	TFO   bool
	MPTCP bool

	// udp
	UDPFragment bool
}

type Listener struct {
	options Options
}

func NewListener(options Options) *Listener {
	return &Listener{
		options: options,
	}
}

func (l *Listener) ListenUDP(ctx context.Context, address string, port uint16) (*net.UDPConn, error) {
	var (
		listenConfig net.ListenConfig
	)

	if l.options.Interface != "" {
		var interfaceFinder = control.NewDefaultInterfaceFinder()
		listenConfig.Control = control.Append(listenConfig.Control, control.BindToInterface(interfaceFinder, l.options.Interface, -1))
	}

	if l.options.ReuseAddr {
		listenConfig.Control = control.Append(listenConfig.Control, control.ReuseAddr())
	}
	if !l.options.UDPFragment {
		listenConfig.Control = control.Append(listenConfig.Control, control.DisableUDPFragment())
	}

	network := meta.Network{Protocol: meta.ProtocolUDP}
	if l.options.Family == constant.FamilyIPv6 {
		network.Version = meta.NetworkVersion6
	} else if l.options.Family == constant.FamilyIPv4 {
		network.Version = meta.NetworkVersion4
	}
	bindAddress := net.JoinHostPort(address, strconv.FormatUint(uint64(port), 10))
	packetConn, err := listenConfig.ListenPacket(ctx, network.String(), bindAddress)

	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	return packetConn.(*net.UDPConn), nil
}

func (l *Listener) ListenTCP(ctx context.Context, address string, port uint16) (net.Listener, error) {
	var (
		listenConfig net.ListenConfig
	)

	if l.options.Interface != "" {
		var interfaceFinder = control.NewDefaultInterfaceFinder()
		listenConfig.Control = control.Append(listenConfig.Control, control.BindToInterface(interfaceFinder, l.options.Interface, -1))
	}

	if l.options.ReuseAddr {
		listenConfig.Control = control.Append(listenConfig.Control, control.ReuseAddr())
	}
	// TODO: customize keepAlive(listen)
	listenConfig.KeepAliveConfig = net.KeepAliveConfig{
		Enable:   true,
		Idle:     constant.DefaultTCPKeepAliveInitial,
		Interval: constant.DefaultTCPKeepAliveInterval,
		Count:    constant.DefaultTCPKeepAliveProbeCount,
	}
	if l.options.MPTCP {
		listenConfig.SetMultipathTCP(true)
	}
	network := meta.Network{Protocol: meta.ProtocolTCP}
	switch l.options.Family {
	case constant.FamilyIPv4:
		network.Version = meta.NetworkVersion6
	case constant.FamilyIPv6:
		network.Version = meta.NetworkVersion4
	}

	var (
		listener    net.Listener
		err         error
		bindAddress = net.JoinHostPort(address, strconv.FormatUint(uint64(port), 10))
	)
	if l.options.TFO {
		var tfoConfig tfo.ListenConfig
		tfoConfig.ListenConfig = listenConfig
		listener, err = tfoConfig.Listen(ctx, network.String(), bindAddress)
	} else {
		listener, err = listenConfig.Listen(ctx, network.String(), bindAddress)
	}

	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	return listener, nil
}
