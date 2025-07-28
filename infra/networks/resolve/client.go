package resolve

import (
	"context"
	"errors"
	"fmt"
	"github.com/daminit/traffics-cli/infra/constant"
	"github.com/daminit/traffics-cli/infra/meta"
	"github.com/miekg/dns"
	"github.com/sagernet/sing/common/task"
	"net"
	"net/netip"
	"time"
)

type RawClient struct {
	dialer      net.Dialer
	destination string
}

func NewRawClient(dialer net.Dialer, destination string) *RawClient {
	return &RawClient{
		dialer:      dialer,
		destination: net.JoinHostPort(destination, "53"),
	}
}

func (c *RawClient) Lookup(ctx context.Context, fqdn string, strategy meta.Strategy) (A []netip.Addr, AAAA []netip.Addr, err error) {
	if fqdn == "" {
		return nil, nil, errors.New("resolve: empty resolve fqdn")
	}

	group := task.Group{}

	if strategy != meta.StrategyIPv6Only {
		group.Append0(func(ctx context.Context) error {
			resp, internal := c.lookupToExchange(ctx, fqdn, dns.TypeA)
			if internal != nil || resp == nil {
				return internal
			}
			A = append(A, resp...)
			return nil
		})
	}
	if strategy != meta.StrategyIPv4Only {
		group.Append0(func(ctx context.Context) error {
			resp, internal := c.lookupToExchange(ctx, fqdn, dns.TypeAAAA)
			if internal != nil || resp == nil {
				return internal
			}
			AAAA = append(AAAA, resp...)
			return nil
		})
	}
	err = group.Run(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve: %w", err)
	}
	//if requestLogger.Enabled(ctx, slog.LevelDebug) {
	//	var attrs []slog.Attr
	//	for _, adr := range append(A, AAAA...) {
	//		var k = ""
	//		if adr.Is6() {
	//			k = "AAAA"
	//		}
	//		if adr.Is4() {
	//			k = "A"
	//		}
	//		if k == "" {
	//			continue
	//		}
	//		attrs = append(attrs, slog.String(k, adr.String()))
	//	}
	//	if len(attrs) > 0 {
	//		requestLogger.DebugContext(ctx, "dns records found", attrs)
	//	}
	//}

	A, AAAA = FilterAddress(A, AAAA, strategy)
	if len(A) == 0 && len(AAAA) == 0 {
		return nil, nil, errors.New(fmt.Sprintf("resolve: no available address found for %s", fqdn))
	}
	return A, AAAA, nil
}

func (c *RawClient) lookupToExchange(ctx context.Context, fqdn string, queryType uint16) (address []netip.Addr, err error) {
	question := &dns.Msg{
		MsgHdr: dns.MsgHdr{
			Id:               dns.Id(),
			RecursionDesired: true,
		},
		Question: []dns.Question{
			{Name: fqdn, Qtype: queryType, Qclass: dns.ClassINET},
		},
	}
	resp, err := c.exchange(
		ctx,
		question,
	)
	if err != nil {
		return nil, err
	}

	return MessageToAddresses(resp)
}

func (c *RawClient) Exchange(ctx context.Context, request *dns.Msg) (answer *dns.Msg, err error) {
	return c.exchange(ctx, request)
}

func (c *RawClient) exchange(ctx context.Context, request *dns.Msg) (answer *dns.Msg, err error) {
	pack, err := request.Pack()
	if err != nil {
		return nil, err
	}

	const maxRetries = 3
	for retry := 0; retry < maxRetries; retry++ {
		conn, err := c.dialer.DialContext(ctx, "udp", c.destination)
		if err != nil {
			if retry == maxRetries-1 {
				return nil, err
			}
			continue
		}
		defer conn.Close()

		if deadline, ok := ctx.Deadline(); ok {
			conn.SetDeadline(deadline)
		} else {
			conn.SetDeadline(time.Now().Add(constant.DefaultResolverReadTimeout))
		}

		if _, err := conn.Write(pack); err != nil {
			if retry == maxRetries-1 {
				return nil, err
			}
			continue
		}

		readBuf := make([]byte, 4096)
		nn, err := conn.Read(readBuf)
		if err != nil {
			if retry == maxRetries-1 {
				return nil, err
			}
			continue
		}

		answer := new(dns.Msg)
		if err := answer.Unpack(readBuf[:nn]); err != nil {
			if retry == maxRetries-1 {
				return nil, err
			}
			continue
		}

		if answer.Id != request.Id {
			continue
		}
		if answer.Rcode != dns.RcodeSuccess {
			return nil, RcodeError(answer.Rcode)
		}

		return answer, nil
	}

	return nil, errors.New("max retries exceeded")
}
