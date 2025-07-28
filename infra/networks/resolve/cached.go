package resolve

import (
	"context"
	"errors"
	"fmt"
	"github.com/daminit/traffics-cli/infra/meta"
	"github.com/miekg/dns"
	"github.com/sagernet/sing/common/cache"
	"github.com/sagernet/sing/common/task"
	"net/netip"
	"time"
)

type cacheResult struct {
	A    []netip.Addr
	AAAA []netip.Addr
}

type CachedResolver struct {
	client Exchanger
	cache  *cache.LruCache[string, cacheResult]
}

func NewCachedResolver(client Exchanger, size int) *CachedResolver {
	return &CachedResolver{
		client: client,
		cache: cache.New[string, cacheResult](
			cache.WithSize[string, cacheResult](size),
			cache.WithAge[string, cacheResult](86400), // max one day
		),
	}
}

func NewCachedResolverDefault(client Exchanger) *CachedResolver {
	return NewCachedResolver(client, 1024)
}

func (c *CachedResolver) Lookup(ctx context.Context, fqdn string, strategy meta.Strategy) (A []netip.Addr, AAAA []netip.Addr, err error) {
	if fqdn == "" {
		return nil, nil, errors.New("resolve: empty resolve fqdn")
	}
	fqdn = dns.Fqdn(fqdn)
	a, aaaa := c.load(fqdn)
	A, AAAA = FilterAddress(a, aaaa, strategy)

	if len(A) != 0 || len(AAAA) != 0 {
		return A, AAAA, nil
	}

	group := task.Group{}

	if strategy != meta.StrategyIPv6Only {
		group.Append0(func(ctx context.Context) error {
			resp, internal := c.lookupToExchange(ctx, fqdn, dns.TypeA)
			if internal != nil || resp == nil {
				return internal
			}
			A = append(A, randomSortAddresses(resp)...)
			return nil
		})
	}
	if strategy != meta.StrategyIPv4Only {
		group.Append0(func(ctx context.Context) error {
			resp, internal := c.lookupToExchange(ctx, fqdn, dns.TypeAAAA)
			if internal != nil || resp == nil {
				return internal
			}
			AAAA = append(AAAA, randomSortAddresses(resp)...)
			return nil
		})
	}

	err = group.Run(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve: exchange failed for %s : %w", fqdn, err)
	}

	A, AAAA = FilterAddress(A, AAAA, strategy)
	if len(A) == 0 && len(AAAA) == 0 {
		return nil, nil, fmt.Errorf("resolve: no available address found for %s", fqdn)
	}

	return A, AAAA, nil
}

func (c *CachedResolver) lookupToExchange(ctx context.Context, fqdn string, queryType uint16) ([]netip.Addr, error) {
	question := &dns.Msg{
		MsgHdr: dns.MsgHdr{
			Id:               dns.Id(),
			RecursionDesired: true,
		},
		Question: []dns.Question{
			{Name: fqdn, Qtype: queryType, Qclass: dns.ClassINET},
		},
	}

	resp, err := c.client.Exchange(
		ctx,
		question,
	)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		panic("client return a nil dns message without error")
	}
	if resp.Rcode != dns.RcodeSuccess {
		return nil, RcodeError(resp.Rcode)
	}
	if resp.Id != question.Id {
		return nil, errors.New("incorrect id")
	}
	if resp.Truncated {
		return nil, errors.New("truncated")
	}

	err = c.store(resp)
	if err != nil {
		return nil, err
	}
	return MessageToAddresses(resp)
}

func (c *CachedResolver) store(msg *dns.Msg) error {
	if msg == nil || len(msg.Question) != 1 || len(msg.Answer) == 0 {
		return errors.New("store a bad message")
	}

	minTTL := uint32(0)
	result := cacheResult{}
	// for _, R := range [][]dns.RR{msg.Answer, msg.Ns, msg.Extra} { // ignore NS and Extra
	for _, rr := range msg.Answer {
		overrideTTL := minTTL == 0 || rr.Header().Ttl > 0 && rr.Header().Ttl < minTTL
		switch record := rr.(type) {
		case *dns.A:
			if overrideTTL {
				minTTL = record.Header().Ttl
			}
			record.Header().Ttl = minTTL
			a, _ := netip.AddrFromSlice(record.A)
			result.A = append(result.A, a)
		case *dns.AAAA:
			if overrideTTL {
				minTTL = record.Header().Ttl
			}
			record.Header().Ttl = minTTL
			a, _ := netip.AddrFromSlice(record.AAAA)
			result.AAAA = append(result.AAAA, a)
		default:
			// discard
		}
	}

	// avoid too small ttl cause cache refresh fast.
	if minTTL <= 4 {
		return nil
	}
	now := time.Now()
	expire := now.Add(time.Duration(minTTL) * time.Second)
	if now.After(expire) {
		return nil
	}
	c.cache.StoreWithExpire(msg.Question[0].Name, result, expire)

	return nil
}

func (c *CachedResolver) load(question string) (A []netip.Addr, AAAA []netip.Addr) {
	response, expire, ok := c.cache.LoadWithExpire(question)
	if !ok {
		return nil, nil
	}
	now := time.Now()
	if now.After(expire) {
		c.cache.Delete(question)
		return nil, nil
	}
	if len(response.A) == 0 && len(response.AAAA) == 0 { // invalid cache
		c.cache.Delete(question)
		return nil, nil
	}
	return response.A, response.AAAA
}
