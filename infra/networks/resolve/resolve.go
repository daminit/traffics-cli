package resolve

import (
	"context"
	"errors"
	"fmt"
	"github.com/daminit/traffics-cli/infra/meta"
	"github.com/miekg/dns"
	"math/rand/v2"
	"net"
	"net/netip"
)

type Resolver interface {
	Lookup(ctx context.Context, fqdn string, strategy meta.Strategy) (A []netip.Addr, AAAA []netip.Addr, err error)
}

type Exchanger interface {
	Exchange(ctx context.Context, msg *dns.Msg) (answer *dns.Msg, err error)
}

type DNSClient interface {
	Resolver
	Exchanger
}

type SystemResolver struct {
}

func NewSystemResolver() *SystemResolver {
	return &SystemResolver{}
}

func (s *SystemResolver) Lookup(ctx context.Context, fqdn string, strategy meta.Strategy) (A []netip.Addr, AAAA []netip.Addr, err error) {
	var errStrategyUnknown = errors.New("network: unknown dns strategy")

	if !strategy.IsValid() {
		return nil, nil, errStrategyUnknown
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
	return randomSortAddresses(A), randomSortAddresses(AAAA), nil
}

func MessageToAddresses(response *dns.Msg) (address []netip.Addr, err error) {
	if response.Rcode != dns.RcodeSuccess {
		return nil, RcodeError(response.Rcode)
	}
	for _, rawAnswer := range response.Answer {
		switch answer := rawAnswer.(type) {
		case *dns.A:
			a, ok := netip.AddrFromSlice(answer.A)
			if !ok {
				continue
			}
			address = append(address, a)
		case *dns.AAAA:
			aaaa, ok := netip.AddrFromSlice(answer.AAAA)
			if !ok {
				continue
			}
			address = append(address, aaaa)
		default:
			// discard others
		}
	}
	return
}

func FilterAddress(A []netip.Addr, AAAA []netip.Addr, strategy meta.Strategy) (
	[]netip.Addr, []netip.Addr) {
	if strategy == meta.StrategyIPv4Only {
		return A, nil
	}
	if strategy == meta.StrategyIPv6Only {
		return nil, AAAA
	}
	return A, AAAA
}

func FqdnToQuestion(fqdn string, strategy meta.Strategy) []dns.Question {
	basic := dns.Question{
		Name:   fqdn,
		Qclass: dns.ClassINET,
	}
	switch strategy {
	case meta.StrategyIPv6Only:
		return []dns.Question{{
			Name:   fqdn,
			Qclass: dns.ClassINET,
			Qtype:  dns.TypeAAAA,
		}}
	case meta.StrategyIPv4Only:
		return []dns.Question{{
			Name:   fqdn,
			Qclass: dns.ClassINET,
			Qtype:  dns.TypeA,
		}}
	default:
		q4, q6 := basic, basic
		q4.Qtype = dns.TypeA
		q6.Qtype = dns.TypeAAAA
		return []dns.Question{
			q4, q6,
		}
	}
}

func randomSortAddresses(raw []netip.Addr) []netip.Addr {
	if len(raw) <= 1 {
		return raw
	}
	var copied []netip.Addr
	copy(copied, raw)
	rand.Shuffle(len(copied), func(i, j int) {
		copied[i], copied[j] = copied[j], copied[i]
	})
	return copied
}

type RcodeError int

func (e RcodeError) Error() string {
	return fmt.Sprintf("resolve: server return rcode %s", dns.RcodeToString[int(e)])
}
