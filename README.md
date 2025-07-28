# Traffics

A simple but powerful port forwarding service

## Overview

Traffics is a lightweight port forwarding tool that supports TCP, UDP, and mixed protocol traffic forwarding. It features simple configuration and high performance.

## Architecture

Traffics uses a Binds (listeners) + Remotes (targets) architecture:

- **Binds**: Local listening configuration, defines ports and protocols to listen on
- **Remotes**: Remote target configuration, defines destination servers for forwarding

Traffic flow: Client → Bind Listener → Remote Target Server

## Installation

```shell
# Clone the project
git clone --depth=1 --branch=main https://github.com/daminit/traffics.git

# Build the project
cd traffics/
mkdir -p bin/ && CGO_ENABLED=0 go build -ldflags="-w -s" -v -o bin/traffics .

# Optional: Install to system PATH
install -o root -g root -m 0755 bin/traffics /usr/bin/traffics
```

## Usage

```shell
$ traffics --help
Usage:
	traffics -l [listen] -r [remote] -c [config] -h
Options:
	-l [listen] : set a listen configuration
	-r [remote] : set a remote configuration
	-c [config] : set the config file path
	--check : check config only (dry-run)
	-h/--help : print help message

Example:
	# Start a forward server from local 9500 to 1.2.3.4:48000
	traffics -l "tcp+udp://:9500?remote=example" -r "example://1.2.3.4:48000"
	
	# start from a config file (this will ignore the command line options like -l and -r)
	traffics -c config.json

See README.md to get full documentation.
```

## Configuration Format

Configuration files support two formats: **URL shorthand** and **complete configuration**, which can be mixed.

### Basic Structure

```json
{
  "log": {
    "level": "info"
  },
  "binds": [
    "tcp+udp://:5353?remote=dns&udp_ttl=60s"
  ],
  "remotes": [
    "dns://1.1.1.1:53?strategy=prefer_ipv4&timeout=10s",
    {
      "name": "web",
      "server": "backend.example.com",
      "port": 80,
      "timeout": "15s"
    }
  ]
}
```

### URL Formats

#### Bind URL
```
protocol://[address]:port?param=value&param=value
```

#### Remote URL
```
name://server:port?param=value&param=value
```

## Configuration Reference

**Note**: URL parameters use the same field names as JSON configuration.

### Log Configuration

- `disable`: Disable logging (default: false)
- `level`: Log level - debug, info, warn, error (default: info)
- `format`: Log format - console,json (default: console)

### Bind Configuration

**Required fields:**
- `listen`: Listen address
- `port`: Listen port
- `remote`: Associated remote service name

**Optional fields:**
- `name`: Bind configuration name
- `network`: Network protocol - tcp, udp, tcp+udp (default: tcp+udp)
- `family`: IP version - 4 or 6
- `interface`: Bind to network interface
- `reuse_addr`: Enable address reuse
- `tfo`: TCP Fast Open
- `mptcp`: Multipath TCP
- `udp_ttl`: UDP connection timeout (default: 60s)
- `udp_buffer_size`: UDP buffer size (default: 65507)
- `udp_fragment`: UDP fragmentation support

### Remote Configuration

**Required fields:**
- `server`: Target server address
- `port`: Target server port
- `name`: Remote service name (corresponds to remote field in bind)

**Optional fields:**
- `dns`: Custom DNS server
- `strategy`: DNS resolution and dial strategy - prefer_ipv4, prefer_ipv6, ipv4_only, ipv6_only
- `interface`: Outbound network interface
- `timeout`: Connection timeout (e.g., "5s")
- `reuse_addr`: Enable address reuse
- `bind_address4`: IPv4 bind address
- `bind_address6`: IPv6 bind address
- `fwmark`: Firewall mark
- `mptcp`: Multipath TCP
- `udp_fragment`: UDP fragmentation support


## Acknowledgments

This project uses the following excellent open-source libraries:

- **[tfo-go](https://github.com/metacubex/tfo-go)** - TCP Fast Open implementation for Go
- **[dns](https://github.com/miekg/dns)** - DNS library in Go
- **[sing](https://github.com/sagernet/sing)** - Universal proxy platform

We are grateful to all contributors and maintainers of these projects for their valuable work.