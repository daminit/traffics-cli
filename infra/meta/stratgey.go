package meta

import "fmt"

var (
	ErrUnknownStrategy = fmt.Errorf("network: unknwon strategy")
	ErrInvalidStrategy = fmt.Errorf("network: invalid strategy")
)

type Strategy uint8

const (
	StrategyDefault    Strategy = iota
	StrategyPreferIPv4          // "prefer_ipv4"
	StrategyPreferIPv6          // "prefer_ipv6"
	StrategyIPv4Only            // "ipv4_only"
	StrategyIPv6Only            // "ipv6_only"
	strategyMax
)

func (s Strategy) String() string {
	switch s {
	case StrategyPreferIPv4:
		return "prefer_ipv4"
	case StrategyPreferIPv6:
		return "prefer_ipv6"
	case StrategyIPv4Only:
		return "ipv4_only"
	case StrategyIPv6Only:
		return "ipv6_only"
	case StrategyDefault:
		return "default"
	default:
		return fmt.Sprintf("strategy: %d", uint8(s))
	}
}

func (s Strategy) IsValid() bool {
	return s < strategyMax
}

func ParseStrategy(s string) (Strategy, error) {
	switch s {
	case "prefer_ipv4":
		return StrategyPreferIPv4, nil
	case "prefer_ipv6":
		return StrategyPreferIPv6, nil
	case "ipv4_only":
		return StrategyIPv4Only, nil
	case "ipv6_only":
		return StrategyIPv6Only, nil
	case "default", "":
		return StrategyDefault, nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnknownStrategy, s)
	}
}
