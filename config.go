package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/daminit/traffics-cli/infra/constant"
	"github.com/daminit/traffics-cli/infra/meta"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Binds  []BindConfig   `json:"binds,omitempty"`
	Remote []RemoteConfig `json:"remotes,omitempty"`
	Log    LogConfig      `json:"log,omitempty"`
}

func NewConfig() Config {
	return Config{
		Binds:  []BindConfig{},
		Remote: []RemoteConfig{},
		Log:    LogConfig{},
	}
}

type LogConfig struct {
	Disable bool   `json:"disable,omitempty"`
	Level   string `json:"level,omitempty"`
	Format  string `json:"format,omitempty"`
}

type BindConfig struct {
	Raw string `json:"-,omitempty"`

	// meta(required)
	Listen string `json:"listen,omitempty"`
	Port   uint16 `json:"port,omitempty"`
	Remote string `json:"remote,omitempty"`
	// meta(optional)
	Name    string            `json:"name,omitempty"`
	Network meta.ProtocolList `json:"network,omitempty"`

	// below is configured by args
	Family    string `json:"family,omitempty"`
	Interface string `json:"interface,omitempty"`
	ReuseAddr bool   `json:"reuse_addr,omitempty"`

	// tcp
	TFO   bool `json:"tfo,omitempty"`
	MPTCP bool `json:"mptcp,omitempty"`

	// udp configuration
	UDPKeepaliveTTL time.Duration `json:"udp_ttl,omitempty"`
	UDPBufferSize   int           `json:"udp_buffer_size,omitempty"` // byte
	UDPFragment     bool          `json:"udp_fragment,omitempty"`
}

type _BindConfig BindConfig

func NewDefaultBind() BindConfig {
	return BindConfig{
		UDPKeepaliveTTL: constant.DefaultUDPKeepAlive,
		UDPBufferSize:   constant.DefaultUDPReadBufferSize,
	}
}

func (c *BindConfig) valid() error {
	if c.UDPBufferSize == 0 {
		return fmt.Errorf("udp buffer size can not be zero")
	}
	if c.UDPKeepaliveTTL == 0 {
		return fmt.Errorf("udp keepalive ttl can not be zero")
	}
	return nil
}

func (c *BindConfig) IsValid() bool {
	return c.valid() == nil
}

func (c *BindConfig) Parse(s string) error {
	if s == "" {
		return errors.New("bind: empty string")
	}

	uu, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("bind: %w", err)
	}
	nc := NewDefaultBind()
	nc.Raw = s
	nc.Listen = uu.Hostname()
	if uu.Port() != "" {
		pp, err := strconv.ParseUint(uu.Port(), 10, 16)
		if err != nil {
			return fmt.Errorf("bind(port): %w", err)
		}
		nc.Port = uint16(pp)
	}

	if uu.Scheme != "" {
		nc.Network = meta.ParseProtocolList(uu.Scheme)
	}

	for k, v := range uu.Query() {
		if len(v) == 0 {
			continue
		}
		pick := len(v) - 1
		var val = v[pick]

		switch k {
		case "family":
			nc.Family = val
		case "interface":
			nc.Interface = val
		case "reuse_addr":
			ok, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("bind(reuse_addr): expected bool, got %s", val)
			}
			nc.ReuseAddr = ok
		case "name":
			nc.Name = val
		case "tfo":
			ok, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("bind(tfo): expected bool, got %s", val)
			}
			nc.TFO = ok
		case "udp_ttl":
			duration, err := time.ParseDuration(val)
			if err != nil {
				return fmt.Errorf("bind(udp_ttl): %w", err)
			}
			nc.UDPKeepaliveTTL = duration
		case "remote":
			nc.Remote = val
		case "udp_buffer_size":
			size, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("bind(udp_buffer_size): %w", err)
			}
			nc.UDPBufferSize = size
		case "udp_fragment":
			ok, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("bind(udp_fragment): expected bool, got %s", val)
			}
			nc.UDPFragment = ok
		case "mptcp":
			ok, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("bind(mptcp): expected bool, got %s", val)
			}
			nc.MPTCP = ok
		default:
			return fmt.Errorf("bind: unknown option: %s", k)
		}
	}
	if err := nc.valid(); err != nil {
		return fmt.Errorf("bind: %w", err)
	}
	*c = nc
	return nil
}

func (c *BindConfig) UnmarshalJSON(bs []byte) error {
	nc := NewDefaultBind()

	rawStr := string(bs)

	if len(rawStr) >= 2 && rawStr[0] == '"' && rawStr[len(rawStr)-1] == '"' {
		rawStr = rawStr[1 : len(rawStr)-1]
	}

	if _, err := url.Parse(rawStr); err == nil && strings.Contains(rawStr, "://") {
		if err := nc.Parse(rawStr); err != nil {
			return err
		}
		*c = nc
		return nil
	}

	nc.Raw = rawStr
	if err := json.Unmarshal(bs, (*_BindConfig)(&nc)); err != nil {
		return err
	}
	if err := nc.valid(); err != nil {
		return err
	}
	*c = nc
	return nil
}

type RemoteConfig struct {
	Raw string `json:"-,omitempty"`

	// meta(required)
	Name   string `json:"name,omitempty"`
	Server string `json:"server,omitempty"`
	Port   uint16 `json:"port,omitempty"`

	// optional
	DNS          string        `json:"dns,omitempty"`
	Strategy     meta.Strategy `json:"strategy,omitempty"`
	Timeout      time.Duration `json:"timeout,omitempty"`
	ReuseAddr    bool          `json:"reuse_addr,omitempty"`
	Interface    string        `json:"interface,omitempty"`
	BindAddress4 netip.Addr    `json:"bind_address4,omitempty"`
	BindAddress6 netip.Addr    `json:"bind_address6,omitempty"`
	FwMark       uint32        `json:"fwmark,omitempty"`

	// tcp
	MPTCP bool `json:"mptcp,omitempty"`

	// udp
	UDPFragment bool `json:"udp_fragment,omitempty"`
}

type _RemoteConfig RemoteConfig

func NewDefaultRemote() RemoteConfig {
	return RemoteConfig{
		Timeout: constant.DefaultDialerTimeout, // default timeout
	}
}

func (c *RemoteConfig) valid() error {
	if c.Name == "" {
		return errors.New("no name specified")
	}
	if c.Server == "" {
		return errors.New("no server specified")
	}
	if c.Port == 0 {
		return errors.New("no server port specified")
	}
	if c.Timeout == 0 {
		return errors.New("timeout must greater than 0")
	}

	return nil
}

func (c *RemoteConfig) IsValid() bool {
	return c.valid() == nil
}

func (c *RemoteConfig) Parse(s string) error {
	if s == "" {
		return errors.New("remote: empty")
	}

	uu, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("remote: %w", err)
	}

	nc := NewDefaultRemote()
	nc.Raw = s
	nc.Server = uu.Hostname()
	nc.Name = uu.Scheme

	if uu.Port() != "" {
		pp, err := strconv.ParseUint(uu.Port(), 10, 16)
		if err != nil {
			return fmt.Errorf("remote(port): %w", err)
		}
		nc.Port = uint16(pp)
	}

	for k, v := range uu.Query() {
		if len(v) == 0 {
			continue
		}
		pick := len(v) - 1
		var val = v[pick]

		switch k {
		case "dns":
			nc.DNS = val
		case "strategy":
			strategy, err := meta.ParseStrategy(val)
			if err != nil {
				return fmt.Errorf("remote(strategy): %w", err)
			}
			nc.Strategy = strategy
		case "timeout":
			timeout, err := time.ParseDuration(v[pick])
			if err != nil {
				return fmt.Errorf("remote(timeout):  expected duration, got %s", val)
			}
			nc.Timeout = timeout
		case "reuse_addr":
			ok, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("remote(reuse_addr):  expected bool, got %s", val)
			}
			nc.ReuseAddr = ok
		case "fwmark":
			mark, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				return fmt.Errorf("remote(fwmark): %w", err)
			}
			nc.FwMark = uint32(mark)
		case "udp_fragment":
			ok, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("remote(udp_fragment): expected bool, got %s", val)
			}
			nc.UDPFragment = ok
		case "interface":
			nc.Interface = val
		case "mptcp":
			mptcp, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("remote(mptcp): expected bool, got %s: %w", val, err)
			}
			nc.MPTCP = mptcp
		case "bind_address4":
			addr, err := netip.ParseAddr(val)
			if err != nil {
				return fmt.Errorf("remote(bind_address4): %w", err)
			}
			nc.BindAddress4 = addr
		case "bind_address6":
			addr, err := netip.ParseAddr(val)
			if err != nil {
				return fmt.Errorf("remote(bind_address6): %w", err)
			}
			nc.BindAddress6 = addr
		case "name":
			nc.Name = val
		default:
			return fmt.Errorf("remote: unknown option: %s", k)
		}
	}
	if err := nc.valid(); err != nil {
		return fmt.Errorf("remote: %w", err)
	}

	*c = nc
	return nil
}

func (c *RemoteConfig) UnmarshalJSON(bs []byte) error {
	nc := NewDefaultRemote()

	rawStr := string(bs)

	if len(rawStr) >= 2 && rawStr[0] == '"' && rawStr[len(rawStr)-1] == '"' {
		rawStr = rawStr[1 : len(rawStr)-1]
	}

	if _, err := url.Parse(rawStr); err == nil && strings.Contains(rawStr, "://") {
		if err := nc.Parse(rawStr); err != nil {
			return err
		}
		*c = nc
		return nil
	}

	nc.Raw = rawStr
	if err := json.Unmarshal(bs, (*_RemoteConfig)(&nc)); err != nil {
		return err
	}

	if err := nc.valid(); err != nil {
		return err
	}
	*c = nc
	return nil
}
