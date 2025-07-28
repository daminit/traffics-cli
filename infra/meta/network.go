package meta

import "strconv"

type NetworkVersion int

const (
	NetworkVersion4    = 4
	NetworkVersion6    = 6
	NetworkVersionDual = 0
)

type Network struct {
	Protocol Protocol
	Version  NetworkVersion
}

func ParseNetwork(network string) (Network, bool) {
	nn := Network{}
	switch network {
	case ProtocolTCP.String(), ProtocolUDP.String(), ProtocolIP.String():
		nn.Version = NetworkVersionDual
		nn.Protocol = Protocol(network)
	case "tcp4", "udp4", "ip4":
		nn.Version = NetworkVersion4
		if len(network) == 3 { // => ip4
			nn.Protocol = ProtocolIP
		} else {
			nn.Protocol = Protocol(network[:3])
		}
	case "tcp6", "udp6", "ip6":
		nn.Version = NetworkVersion6
		if len(network) == 3 { // => ip6
			nn.Protocol = ProtocolIP
		} else {
			nn.Protocol = Protocol(network[:3])
		}
	default:
		return Network{}, false
	}
	return nn, true
}

func (n Network) String() string {
	if n.Version == NetworkVersionDual {
		return n.Protocol.String()
	}
	return n.Protocol.String() + strconv.Itoa(int(n.Version))
}
