package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/daminit/traffics-cli/infra/constant"
	"github.com/daminit/traffics-cli/infra/logging"
	"github.com/daminit/traffics-cli/infra/meta"
	"github.com/daminit/traffics-cli/infra/networks/dialer"
	"github.com/daminit/traffics-cli/infra/networks/listener"
	"github.com/daminit/traffics-cli/infra/networks/resolve"
	"github.com/daminit/traffics-cli/proxy/inbounds"
	"github.com/daminit/traffics-cli/proxy/outbounds"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strconv"
	"syscall"
	"time"
)

type Traffics struct {
	ctx    context.Context
	cancel context.CancelFunc

	config         Config
	logger         *slog.Logger
	nameToOutbound map[string]*outbounds.Outbound
	nameToInbound  map[string]*inbounds.Inbound
	udpConnTrack   map[netip.AddrPort]UDPConnWrapper
}

func NewTraffics(config Config) (*Traffics, error) {
	t := &Traffics{}
	t.config = config

	t.nameToOutbound = make(map[string]*outbounds.Outbound)
	t.nameToInbound = make(map[string]*inbounds.Inbound)
	t.udpConnTrack = make(map[netip.AddrPort]UDPConnWrapper)

	var err error
	t.logger, err = newLogger(config.Log)
	if err != nil {
		return nil, fmt.Errorf("traffics(logger): %w", err)
	}

	if len(config.Remote) == 1 && len(config.Binds) == 1 && config.Binds[0].Remote == "" {
		config.Binds[0].Remote = config.Remote[0].Name
	}

	err = t.initOutbound() // init outbounds first
	if err != nil {
		return nil, fmt.Errorf("traffics(init_outbound): %w", err)
	}
	err = t.initInbound()
	if err != nil {
		return nil, fmt.Errorf("traffics(init_inbound): %w", err)
	}

	return t, nil
}

func (t *Traffics) Close() error {
	t.cancel()
	for _, c := range t.udpConnTrack {
		c.Conn.Close()
	}
	for _, c := range t.nameToInbound {
		c.Close()
	}
	return nil
}

func (t *Traffics) Start(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)
	for _, in := range t.nameToInbound {
		err := in.Start(t.ctx)
		if err != nil {
			return fmt.Errorf("traffics(start): %w", err)
		}
	}
	return nil
}

func (t *Traffics) initOutbound() error {
	var (
		defaultResolver resolve.Resolver = resolve.NewCachedResolverFromResolver(resolve.NewSystemResolver(),
			constant.DefaultResolverCacheSize, constant.DefaultResolverCacheTTL)
	)

	// build dialer first
	for _, v := range t.config.Remote {
		if v.Name == "" {
			// TODO: provide more detailed info about this
			return fmt.Errorf("no name specified for %s", v.Server)
		}

		if _, ok := t.nameToOutbound[v.Name]; ok {
			return fmt.Errorf("duplicated remote name: %s", v.Name)
		}

		realResolver := defaultResolver
		if v.DNS != "" {
			realResolver = resolve.NewCachedResolverFromExchanger(
				resolve.NewRawClient(net.Dialer{}, v.DNS), constant.DefaultResolverCacheSize)
		}
		var bind4, bind6 netip.Addr
		bind4 = v.BindAddress4
		bind6 = v.BindAddress6

		dd, err := dialer.NewDefault(dialer.DialConfig{
			Resolver:     realResolver,
			Timeout:      cmp.Or(v.Timeout, constant.DefaultDialerTimeout),
			Interface:    v.Interface,
			BindAddress4: bind4,
			BindAddress6: bind6,
			FwMark:       v.FwMark,
			ReuseAddr:    v.ReuseAddr,
			MPTCP:        v.MPTCP,
			UDPFragment:  v.UDPFragment,
			Strategy:     v.Strategy,
		})
		if err != nil {
			return err
		}
		t.nameToOutbound[v.Name] = &outbounds.Outbound{
			Dialer:  dd,
			Address: net.JoinHostPort(v.Server, strconv.FormatUint(uint64(v.Port), 10)),
			Logger:  t.logger.With(slog.String("remote", v.Name)),
		}
	}
	return nil
}

func (t *Traffics) initInbound() error {
	// parse listener
	for _, v := range t.config.Binds {

		var name = v.Name
		if v.Name == "" {
			name = "(" + net.JoinHostPort(v.Listen, strconv.FormatUint(uint64(v.Port), 10)) + ")"
		}
		if _, exist := t.nameToInbound[name]; exist {
			return fmt.Errorf("duplicated bind: %s", name)
		}

		if v.Remote == "" {
			return fmt.Errorf("no remote specified for %s", name)
		}

		logger := t.logger.With(slog.String("listener", name))
		outbound, ok := t.nameToOutbound[v.Remote]
		if !ok {
			return fmt.Errorf("remote not found with name: %s", v.Remote)
		}

		li := listener.NewListener(listener.Options{
			Family:      v.Family,
			Interface:   v.Interface,
			ReuseAddr:   v.ReuseAddr,
			TFO:         v.TFO,
			MPTCP:       v.MPTCP,
			UDPFragment: v.UDPFragment,
		})

		inbound := &inbounds.Inbound{
			Logger:        logger,
			Listener:      li,
			Protocols:     v.Network,
			Address:       v.Listen,
			Port:          v.Port,
			UDPBufferSize: cmp.Or(v.UDPBufferSize, constant.DefaultUDPReadBufferSize),
		}

		inbound.PacketHandler = (*TrafficHandler)(t).PacketHandler(
			v.Network.ContainsProtocol(meta.ProtocolUDP),
			inbound, outbound, v.UDPKeepaliveTTL)
		inbound.ConnHandler = (*TrafficHandler)(t).ConnHandler(
			v.Network.ContainsProtocol(meta.ProtocolTCP),
			inbound, outbound)

		t.nameToInbound[name] = inbound
	}
	return nil
}

type TrafficHandler Traffics

type UDPConnWrapper struct {
	Logger     logging.ContextLogger
	Writer     inbounds.PacketWriter
	Conn       *net.UDPConn
	ReadBuffer *buf.Buffer
}

func (c *UDPConnWrapper) Close() {
	c.Conn.Close()
	c.ReadBuffer.Release()
}

func (t *TrafficHandler) PacketHandler(
	enable bool,
	in *inbounds.Inbound,
	out *outbounds.Outbound,
	ttl time.Duration,
) inbounds.PacketHandler {
	if !enable {
		return nil
	}

	var newUDPConn = func(conn *net.UDPConn) UDPConnWrapper {
		return UDPConnWrapper{
			Logger:     in.Logger.With(logging.AttrIdRandom()),
			Writer:     in,
			Conn:       conn,
			ReadBuffer: buf.NewSize(in.UDPBufferSize),
		}
	}

	return inbounds.FuncPacketHandler(func(p []byte, remote netip.AddrPort, pw inbounds.PacketWriter) {
		if connWrapper, hit := t.udpConnTrack[remote]; hit {
			_, err := connWrapper.Conn.Write(p)
			if err != nil {
				connWrapper.Logger.ErrorContext(t.ctx, "write message error",
					logging.AttrError(err))
			}
			return
		}

		conn, err := out.DialContext(t.ctx, string(meta.ProtocolUDP))
		if err != nil {
			in.Logger.ErrorContext(t.ctx, "dial new udp connection failed",
				logging.AttrError(err),
			)
			return
		}

		if udpConn, ok := conn.(*net.UDPConn); ok {
			newConn := newUDPConn(udpConn)
			t.udpConnTrack[remote] = newConn

			go t.newUdpLoop(remote, newConn, ttl)
			if newConn.Logger.Enabled(t.ctx, slog.LevelDebug) {
				newConn.Logger.DebugContext(t.ctx, "new udp connection established",
					slog.String("source", remote.String()),
					slog.String("remote", udpConn.RemoteAddr().String()),
					slog.String("local", udpConn.LocalAddr().String()),
				)
			}

			_, err = udpConn.Write(p)
			if err != nil {
				newConn.Logger.ErrorContext(t.ctx, "write udp message failed",
					logging.AttrError(err))
			}
		} else {
			panic("DialContext in udp network returned a non-udpConn")
		}
	})
}

func (t *TrafficHandler) newUdpLoop(
	client netip.AddrPort,
	proxyConn UDPConnWrapper,
	ttl time.Duration,
) {
	defer func() {
		delete(t.udpConnTrack, client)
		proxyConn.Close()

		proxyConn.Logger.DebugContext(t.ctx, "udp connection closed")
	}()

	conn := proxyConn.Conn
	buffer := proxyConn.ReadBuffer

	for {
		buffer.Reset()
		conn.SetReadDeadline(time.Now().Add(ttl))
	again:
		read, err := conn.Read(buffer.FreeBytes())
		buffer.Truncate(read)

		if read == 0 && err == nil {
			panic("seems like the udp buffer size is zero: see https://github.com/golang/go/issues/23849")
		}
		if err != nil {
			var ope *net.OpError
			if errors.As(err, &ope) && errors.Is(ope.Err, syscall.ECONNREFUSED) {
				// This will happen if the last write failed
				// (e.g: nothing is actually listening on the
				// proxied port on the container), ignore it
				// and continue until UDPKeepaliveTTL
				// expires:
				goto again
			}
			return
		}
		if read != 0 {
			proxyConn.Writer.WritePacket(buffer.Bytes(), client)
		}
	}
}

func (t *TrafficHandler) ConnHandler(
	enable bool,
	in *inbounds.Inbound,
	out *outbounds.Outbound,
) inbounds.ConnHandler {
	if !enable {
		return nil
	}

	return inbounds.FuncConnHandler(func(ctx context.Context, local net.Conn) {
		connLogger := in.Logger.With(logging.AttrIdRandom())
		defer local.Close()
		remote, err := out.DialContext(ctx, string(meta.ProtocolTCP))
		if err != nil {
			connLogger.ErrorContext(ctx, "dial new tcp connection failed", logging.AttrError(err))
			return
		}
		defer remote.Close()

		if connLogger.Enabled(ctx, slog.LevelDebug) {
			connLogger.DebugContext(ctx, "new tcp connection established",
				slog.String("source", local.RemoteAddr().String()),
				slog.String("remote", remote.RemoteAddr().String()),
				slog.String("local", remote.LocalAddr().String()),
			)
		}

		if err = bufio.CopyConn(ctx, local, remote); err != nil {
			connLogger.ErrorContext(ctx, "copy connections aborted", logging.AttrError(err))
		}
		connLogger.DebugContext(ctx, "connection closed")
	})
}

func newLogger(config LogConfig) (*slog.Logger, error) {
	if config.Disable {
		return slog.New(slog.DiscardHandler), nil
	}

	var logger *slog.Logger
	level := slog.Level(0)
	if config.Level != "" {
		err := level.UnmarshalText([]byte(config.Level))
		if err != nil {
			return nil, err
		}
	}

	if config.Format == "console" || config.Format == "" {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	} else if config.Format == "json" {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	} else {
		return nil, fmt.Errorf("invalid log format: %s", config.Format)
	}

	return logger, nil
}
