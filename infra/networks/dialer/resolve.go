package dialer

import (
	"context"
	"github.com/daminit/traffics-cli/infra/meta"
	"github.com/miekg/dns"
	"net"
	"net/netip"
)

type Resolver interface {
	Lookup(ctx context.Context, fqdn string, strategy meta.Strategy) (A []netip.Addr, AAAA []netip.Addr, err error)
}

var DefaultResolver = &defaultResolver{}

type defaultResolver struct{}

func (s *defaultResolver) Lookup(ctx context.Context, fqdn string, strategy meta.Strategy) (A []netip.Addr, AAAA []netip.Addr, err error) {
	if !strategy.IsValid() {
		return nil, nil, meta.ErrInvalidStrategy
	}
	fqdn = dns.Fqdn(fqdn)
	answer, err := net.DefaultResolver.LookupIPAddr(ctx, fqdn)
	if err != nil {
		return nil, nil, err
	}
	for _, addr := range answer {
		netipip, ok := netip.AddrFromSlice(addr.IP)
		if !ok || !netipip.IsValid() {
			continue
		}

		if strategy != meta.StrategyIPv6Only && netipip.Is4() {
			A = append(A, netipip)
		}
		if strategy != meta.StrategyIPv4Only && netipip.Is6() {
			AAAA = append(AAAA, netipip)
		}
	}
	return A, AAAA, nil
}
