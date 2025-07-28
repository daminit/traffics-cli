package outbounds

import (
	"context"
	"github.com/daminit/traffics-cli/infra/networks/dialer"
	"log/slog"
	"net"
)

type Outbound struct {
	Logger  *slog.Logger
	Dialer  dialer.Dialer
	Address string
}

func (o *Outbound) DialContext(ctx context.Context, network string) (net.Conn, error) {
	o.Logger.InfoContext(ctx, "new connection",
		slog.String("network", network),
	)
	return o.Dialer.DialContext(ctx, network, o.Address)
}
