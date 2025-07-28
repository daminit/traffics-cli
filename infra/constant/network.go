package constant

import "time"

const (
	DefaultTCPKeepAliveInitial    = 10 * time.Minute
	DefaultTCPKeepAliveInterval   = 75 * time.Second
	DefaultTCPKeepAliveProbeCount = 16

	DefaultDialerTimeout       = 5 * time.Second
	DefaultResolverReadTimeout = 5 * time.Second
	DefaultUDPReadBufferSize   = 65507
	DefaultUDPKeepAlive        = 60 * time.Second
)

const (
	FamilyIPv4 = "4"
	FamilyIPv6 = "6"
)
