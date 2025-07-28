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
	resolver  Resolver
	exchanger Exchanger
	cache     *cache.LruCache[string, cacheResult]
	ttl       int
}

func NewCachedResolverFromExchanger(client Exchanger, size int) *CachedResolver {
	if size < 4 {
		panic("too small")
	}
	return &CachedResolver{
		exchanger: client,
		cache: cache.New[string, cacheResult](
			cache.WithSize[string, cacheResult](size),
			cache.WithAge[string, cacheResult](86400), // max one day
		),
	}
}

func NewCachedResolverFromResolver(client Resolver, size int, ttl int) *CachedResolver {
	if size < 4 || ttl < 4 {
		panic("too small")
	}
	if _, ok := client.(*CachedResolver); ok {
		panic("nested CachedResolver")
	}

	return &CachedResolver{
		resolver: client,
		ttl:      ttl,
		cache: cache.New[string, cacheResult](
			cache.WithSize[string, cacheResult](size),
			cache.WithAge[string, cacheResult](86400), // max one day
		),
	}
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
	if c.exchanger != nil { // use exchanger to get more detailed info
		A, AAAA, err = c.newExchange(ctx, fqdn, strategy)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve: %w", err)
		}
	} else if c.resolver != nil && c.ttl != 0 {
		A, AAAA, err = c.resolver.Lookup(ctx, fqdn, strategy)
		err = c.storeLookup(fqdn, A, AAAA)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve: %w", err)
		}
	} else {
		panic("both Exchanger and Resolver not found or not configured.")
	}

	A, AAAA = FilterAddress(A, AAAA, strategy)
	if len(A) == 0 && len(AAAA) == 0 {
		return nil, nil, fmt.Errorf("resolve: no available address found for %s", fqdn)
	}

	return A, AAAA, nil
}

func (c *CachedResolver) newExchange(ctx context.Context, fqdn string, strategy meta.Strategy) (A, AAAA []netip.Addr, err error) {
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
		return nil, nil, fmt.Errorf("exchange failed for %s : %w", fqdn, err)
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

	resp, err := c.exchanger.Exchange(
		ctx,
		question,
	)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		panic("exchanger return a nil dns message without error")
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

	err = c.storeMsg(resp)
	if err != nil {
		return nil, err
	}
	return MessageToAddresses(resp)
}

func (c *CachedResolver) storeLookup(fqdn string, A, AAAA []netip.Addr) error {
	if len(A) == 0 && len(AAAA) == 0 || !dns.IsFqdn(fqdn) {
		return fmt.Errorf("store a bad lookup result")
	}

	expire := time.Now().Add(time.Duration(c.ttl) * time.Second)
	result := cacheResult{
		A:    A,
		AAAA: AAAA,
	}
	c.cache.StoreWithExpire(fqdn, result, expire)
	return nil
}

func (c *CachedResolver) storeMsg(msg *dns.Msg) error {
	if msg == nil || len(msg.Question) != 1 || len(msg.Answer) == 0 {
		return fmt.Errorf("store a bad message")
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
