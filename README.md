# SOCKS5 Proxy Server

A lightweight and secure SOCKS5 proxy server implementation in Go with IP-based access control.

## Features

- Full SOCKS5 protocol support for TCP connections
- IP-based access control using CIDR notation
- Support for both IPv4 and IPv6 addresses
- Domain name resolution support
- Debug mode for troubleshooting
- No authentication required (designed for trusted networks)

## Installation

```bash
# Clone the repository
git clone https://github.com/yourusername/proxy.git
cd proxy

# Build the binary
go build
```

## Usage

Start the proxy server:

```bash
# Start with default settings
./proxy

# Start with debug mode enabled
./proxy -debug
```

The server will listen on the port specified in config.json (default: 1080).

## Configuration

The server uses a `config.json` file to specify allowed IP ranges in CIDR notation. Example configuration:

```json
{
  "port": 1080,
  "ipv4": [
    "127.0.0.1/32",
    "192.168.0.0/16",
    "10.0.0.0/8"
  ],
  "ipv6": [
    "::1/128",
    "fe80::/10"
  ]
}
```

Configuration options:
- `port`: The TCP port number the proxy server will listen on (default: 1080)
- `ipv4`: List of allowed IPv4 CIDR ranges
- `ipv6`: List of allowed IPv6 CIDR ranges

Only clients with IP addresses within these ranges will be allowed to connect to the proxy server.

## Debug Mode

Start the server with the `-debug` flag to enable verbose logging:

```bash
./proxy -debug
```

This will output detailed information about:
- Client connections and IP verification
- SOCKS5 protocol negotiations
- Connection establishment status
- Data transfer errors

## Protocol Support

The server implements the SOCKS5 protocol with the following features:
- No authentication required
- TCP CONNECT command support
- IPv4, IPv6, and domain name address types

## Security Considerations

- The server does not implement authentication - access control is purely IP-based
- Only TCP CONNECT method is supported (no UDP or BIND)
- Configure your firewall to restrict access to port 1080
- Regularly review and update the allowed IP ranges in config.json

## Testing

Run the test suite:

```bash
go test
```
