package logging

import (
	"log/slog"
	"math/rand/v2"
)

const (
	LogKeyError = "error"
	LogKeyZone  = "zone"
)

func AttrError(err error) slog.Attr {
	if err == nil {
		panic("nil error")
	}
	return slog.String(LogKeyError, err.Error())
}
func AttrZone(zone string) slog.Attr {
	if zone == "" {
		panic("empty zone")
	}
	return slog.String(LogKeyZone, zone)
}
func AttrId(id uint64) slog.Attr {
	return slog.Uint64("id", id)
}

func AttrIdRandom() slog.Attr {
	return AttrId(rand.Uint64())
}
