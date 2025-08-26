package inbounds

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"

	"github.com/daminit/traffics-cli/infra/constant"
	"github.com/daminit/traffics-cli/infra/logging"
	"github.com/daminit/traffics-cli/infra/meta"
	"github.com/daminit/traffics-cli/infra/networks/listener"
	"github.com/sagernet/sing/common"
)

type PacketWriter interface {
	WritePacket(bs []byte, remote netip.AddrPort)
}

type PacketHandler interface {
	HandlePacket(p []byte, remote netip.AddrPort, pw PacketWriter)
}

type ConnHandler interface {
	HandleConn(ctx context.Context, conn net.Conn)
}

type (
	FuncPacketHandler func(p []byte, remote netip.AddrPort, pw PacketWriter)
	FuncConnHandler   func(ctx context.Context, conn net.Conn)
)

func (f FuncPacketHandler) HandlePacket(p []byte, remote netip.AddrPort, pw PacketWriter) {
	f(p, remote, pw)
}
func (f FuncConnHandler) HandleConn(ctx context.Context, conn net.Conn) {
	f(ctx, conn)
}

type Inbound struct {
	ctx      context.Context
	Logger   *slog.Logger
	Listener *listener.Listener

	// configurations
	Protocols     meta.ProtocolList
	Address       string
	Port          uint16
	UDPBufferSize int

	// Handler
	PacketHandler PacketHandler
	ConnHandler   ConnHandler

	// internal
	udpConn     *net.UDPConn
	tcpListener net.Listener
	cancel      context.CancelFunc
}

func (o *Inbound) Start(ctx context.Context) error {
	o.ctx, o.cancel = context.WithCancel(ctx)

	var err error
	if o.Protocols.Contains(string(meta.ProtocolTCP)) {
		if o.ConnHandler == nil {
			return fmt.Errorf("inbounds: ConnHandler required")
		}
		o.tcpListener, err = o.Listener.ListenTCP(o.ctx, o.Address, o.Port)
		if err != nil {
			return fmt.Errorf("inbounds: %w", err)
		}
		o.Logger.InfoContext(o.ctx, "new tcp server started",
			slog.String("address", o.tcpListener.Addr().String()))
		go o.loopTcp()
	}
	if o.Protocols.Contains(string(meta.ProtocolUDP)) {
		if o.PacketHandler == nil {
			return fmt.Errorf("inbounds: PacketHandler required")
		}
		o.udpConn, err = o.Listener.ListenUDP(o.ctx, o.Address, o.Port)
		if err != nil {
			return fmt.Errorf("inbounds: %w", err)
		}

		go o.loopUdpIn()
		o.Logger.InfoContext(o.ctx, "new udp server started",
			slog.String("address", o.udpConn.LocalAddr().String()))
	}
	return nil
}

func (o *Inbound) loopUdpIn() {
	bufferSize := cmp.Or(o.UDPBufferSize, constant.DefaultUDPReadBufferSize)
	buf := make([]byte, bufferSize)
	for {
		n, remote, err := o.udpConn.ReadFromUDPAddrPort(buf[0:bufferSize])
		if err == nil && n == 0 {
			panic("seems like the udp buffer size is zero: see https://github.com/golang/go/issues/23849")
		}
		if err != nil {
			if common.Done(o.ctx) {
				return
			}
			o.Logger.ErrorContext(o.ctx, "read udp message", slog.String("error", err.Error()))
			continue
		}
		if !remote.IsValid() {
			o.Logger.ErrorContext(o.ctx, "invalid address")
			continue
		}
		o.PacketHandler.HandlePacket(buf[:n], remote, o)
	}
}

func (o *Inbound) WritePacket(bs []byte, remote netip.AddrPort) {
	_, err := o.udpConn.WriteToUDPAddrPort(bs[:], remote)
	if err != nil {
		o.Logger.ErrorContext(o.ctx, "write udp message", logging.AttrError(err))
	}
}

func (o *Inbound) loopTcp() {
	for {
		conn, err := o.tcpListener.Accept()
		if err != nil {
			if common.Done(o.ctx) {
				return
			}
			o.Logger.ErrorContext(o.ctx, "an error occurred while accept",
				logging.AttrError(err))
			continue
		}
		go o.ConnHandler.HandleConn(o.ctx, conn)
	}
}

func (o *Inbound) Close() error {
	o.cancel()
	if o.tcpListener != nil {
		o.tcpListener.Close()
	}
	if o.udpConn != nil {
		o.udpConn.Close()
	}
	return nil
}
