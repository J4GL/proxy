# Multi-Protocol Proxy Server

A high-performance Go proxy server that handles both HTTP/HTTPS and SOCKS5 protocols on a single port with real-time monitoring capabilities.

## Features

- **Dual Protocol Support**: Automatically detects and handles both HTTP/HTTPS and SOCKS5 connections
- **Real-time Monitoring**: Web dashboard with live connection tracking via WebSocket
- **IP-based Access Control**: Configurable whitelist for authorized clients
- **Protocol Sniffing**: Intelligent detection of connection type without separate ports
- **Comprehensive Logging**: Debug mode with detailed connection information

## Quick Start

### Prerequisites

- Go 1.18 or later
- Basic networking tools (curl, wget) for testing

### Installation

1. Clone the repository:
```bash
git clone <repository-url>
cd proxy
```

2. Build the applications:
```bash
go build -o proxy_app main.go
go build -o test_server/test_server_app test_server/main.go
```

### Basic Usage

1. **Start the proxy server:**
```bash
./proxy_app
```

2. **Start the test server** (in another terminal):
```bash
cd test_server && ./test_server_app
```

3. **Test the proxy:**
```bash
# HTTP proxy
curl --proxy http://localhost:8080 http://localhost:8081/test.txt

# SOCKS5 proxy
curl --proxy socks5://localhost:8080 http://localhost:8081/test.txt
```

## Configuration

### Access Control

Edit `config.yaml` to configure allowed IP addresses:

```yaml
allowed_ips:
  - "127.0.0.1"
  - "::1"
  - "your.ip.address.here"
```

### Command Line Options

```bash
./proxy_app [OPTIONS]

Options:
  -debug, -d              Enable debug logging
  -monitor-port, -m PORT  Set monitoring dashboard port (default: 8082)
```

## Monitoring Dashboard

The proxy server includes a comprehensive real-time monitoring interface that provides visibility into all proxy connections.

### Web Interface

Access the monitoring dashboard at `http://localhost:8082` (or your configured monitoring port):

**Features:**
- **Real-time Connection Tracking**: View all active connections with live updates
- **Connection Statistics**: Total connections processed since startup
- **Protocol Breakdown**: See HTTP vs SOCKS5 connection distribution
- **Client Information**: Monitor client IP addresses and connection patterns
- **Connection Duration**: Track how long connections remain active
- **Destination Mapping**: View what destinations clients are accessing

**Dashboard Elements:**
- Statistics cards showing total and active connection counts
- Live connection table with client IP, protocol, destination, and duration
- Real-time updates via WebSocket (no page refresh needed)
- Connection status indicators and timestamps
- Clean, responsive interface that works on desktop and mobile

### API Endpoints

The monitoring system provides both web interface and programmatic access:

- `GET /` - Interactive web dashboard
- `GET /api/stats` - JSON statistics for integration with external tools
- `WebSocket /ws` - Real-time updates stream for custom applications

### Monitoring Configuration

The monitoring server runs on a separate port (default 8082) and can be configured:

```bash
# Custom monitoring port
./proxy_app -monitor-port 9090

# Access dashboard at http://localhost:9090
```

## Testing

### Automated Testing

Run the comprehensive test suite:

```bash
./test.sh
```

This script automatically:
- Builds the applications if needed
- Starts both servers
- Runs tests for HTTP and SOCKS5 protocols
- Cleans up processes after testing

### Manual Testing

Test HTTP proxy:
```bash
curl --proxy http://localhost:8080 https://httpbin.org/ip
wget -e "http_proxy=http://localhost:8080" https://httpbin.org/ip -O -
```

Test SOCKS5 proxy:
```bash
curl --proxy socks5://localhost:8080 https://httpbin.org/ip
all_proxy=socks5://localhost:8080 wget https://httpbin.org/ip -O -
```

### Go Tests

Run monitoring system tests:
```bash
go test -v -run TestMonitoring
```

## Architecture

### Protocol Detection

The server uses protocol sniffing to handle multiple protocols on a single port:
- SOCKS5 connections start with byte `0x05`
- All other connections are treated as HTTP

### Core Components

- **Connection Handler**: Manages incoming connections and protocol detection
- **HTTP Handler**: Processes HTTP/HTTPS requests with CONNECT method support
- **SOCKS5 Handler**: Implements full SOCKS5 protocol with authentication
- **Monitoring System**: Thread-safe connection tracking with WebSocket broadcasting

### Security Features

- IP-based access control
- No authentication bypass vulnerabilities
- Secure connection handling with proper cleanup
- Debug logging for security auditing

## Development

### Project Structure

```
├── main.go              # Main proxy server
├── config.yaml          # IP whitelist configuration
├── test_server/         # Test HTTP server
│   ├── main.go         # Test server implementation
│   └── test.txt        # Test file
├── monitoring_test.go   # Monitoring system tests
├── test.sh             # Automated test suite
└── CLAUDE.md           # Development guide
```

### Key Functions

- `handleConnection()` - Main connection processing
- `handleHTTP()` - HTTP/HTTPS proxy logic
- `handleSocks5()` - SOCKS5 implementation
- `addConnection()` - Connection tracking
- `broadcastUpdate()` - Real-time monitoring updates

## Dependencies

- `github.com/gorilla/websocket` - WebSocket support
- `gopkg.in/yaml.v2` - YAML configuration parsing
- Standard Go libraries for networking

## Troubleshooting

### Common Issues

1. **Port already in use**: Check if other services are using ports 8080, 8081, or 8082
2. **Connection refused**: Verify IP address is in the whitelist
3. **Monitoring not working**: Ensure WebSocket connections are not blocked

### Debug Mode

Enable debug logging to troubleshoot connection issues:

```bash
./proxy_app -debug
```

This provides detailed information about:
- Client connection attempts
- Protocol detection results
- Connection establishment status
- Data relay operations

## License

This project is provided as-is for educational and development purposes.