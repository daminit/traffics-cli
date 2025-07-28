package meta

import (
	"encoding/json"
	"errors"
	"github.com/daminit/traffics-cli/infra/utils/uslice"
	"slices"
	"strings"
)

type Protocol string

const (
	ProtocolTCP Protocol = "tcp"
	ProtocolUDP Protocol = "udp"
	ProtocolIP  Protocol = "ip"
)

func (p Protocol) String() string {
	return string(p)
}

func ParseProtocol(protocol string) Protocol {
	if len(protocol) == 0 {
		return ""
	}
	pp := Protocol(protocol)
	switch pp {
	case ProtocolTCP, ProtocolUDP, ProtocolIP:
		return pp
	default:
		return ""
	}
}

type ProtocolList []Protocol

var emptyProtocolList = ProtocolList{}

func (n ProtocolList) Contains(network string) bool {
	nn, ok := ParseNetwork(network)
	return ok && slices.Contains(n, nn.Protocol)
}

func (n ProtocolList) ContainsProtocol(protocol Protocol) bool {
	return ParseProtocol(protocol.String()) != "" && slices.Contains(n, protocol)
}

func (n ProtocolList) IsValid() bool {
	return len(n) != 0 && !slices.ContainsFunc(n, func(p Protocol) bool {
		return ParseProtocol(p.String()) == ""
	})
}

func (n *ProtocolList) UnmarshalJSON(bs []byte) error {
	// accept
	// // "tcp+udp" or "tcp" or "ip"
	// // ["tcp","udp"]

	nn := ProtocolList{}
	if len(bs) < 2 {
		return errors.New("too short")
	}
	if bs[0] == '"' && bs[len(bs)-1] == '"' { // string style
		nn = ParseProtocolList(string(bs[1 : len(bs)-1]))
		*n = nn
		return nil
	}
	if bs[0] == '[' && bs[len(bs)-1] == ']' { // json array style
		var sn []string
		err := json.Unmarshal(bs, &sn)
		if err != nil {
			return err
		}
		for i := 0; i < len(sn); i++ {
			p := sn[i]
			nn = append(nn, ParseProtocolList(p)...)
		}
		nn = uslice.Unique(nn)
		if len(nn) == 0 {
			*n = emptyProtocolList
			return nil
		}
		*n = nn
		return nil
	}
	return errors.New("bad json format")
}

func ParseProtocolList(name string) ProtocolList {
	if len(name) == 0 {
		return emptyProtocolList
	}

	if pp := ParseProtocol(name); pp != "" {
		return []Protocol{pp}
	}

	multi := strings.Split(name, "+")
	if len(multi) >= 1 {
		ret := ProtocolList{}
		multi = uslice.Unique(multi)
		for _, p := range multi {
			if pp := ParseProtocol(p); pp != "" {
				ret = append(ret, pp)
			}
		}
		if len(ret) == 0 {
			return emptyProtocolList
		}
		return ret
	}

	return emptyProtocolList
}
