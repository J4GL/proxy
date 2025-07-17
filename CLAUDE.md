# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go-based multi-protocol proxy server that supports both HTTP/HTTPS and SOCKS5 protocols on a single port (8080). The server includes IP-based access control, real-time monitoring via WebSocket, and a web dashboard for connection tracking.

## Key Architecture

### Protocol Detection
- Uses protocol sniffing by peeking at the first byte of incoming connections
- SOCKS5 connections start with `0x05`, all others treated as HTTP
- Single listener handles both protocols seamlessly

### Core Components
- **main.go**: Contains the entire proxy server implementation
- **config.yaml**: IP whitelist configuration
- **test_server/**: Simple HTTP server for testing proxy functionality
- **monitoring_test.go**: Go tests for monitoring functionality

### Monitoring System
- Real-time WebSocket dashboard on port 8082 (configurable with `-monitor-port`)
- REST API endpoint at `/api/stats` for connection statistics
- HTML dashboard at root path with live connection tracking
- Thread-safe connection tracking with unique IDs

## Common Commands

### Building and Running
```bash
# Build proxy server
go build -o proxy_app main.go

# Build test server
go build -o test_server/test_server_app test_server/main.go

# Run proxy server (default ports: 8080 for proxy, 8082 for monitoring)
./proxy_app

# Run with debug logging
./proxy_app -debug

# Run with custom monitoring port
./proxy_app -monitor-port 9090

# Run test server
cd test_server && ./test_server_app
```

### Testing
```bash
# Run comprehensive test suite
./test.sh

# Run Go tests for monitoring
go test -v -run TestMonitoring

# Manual testing with curl
curl --proxy http://localhost:8080 http://localhost:8081/test.txt
curl --proxy socks5://localhost:8080 http://localhost:8081/test.txt
```

## Configuration

### IP Access Control
- Edit `config.yaml` to modify allowed IP addresses
- Default allows localhost (127.0.0.1, ::1) and one additional IP
- Server must be restarted after configuration changes

### Ports
- Proxy server: 8080 (hardcoded in `proxyPort` constant)
- Monitoring dashboard: 8082 (configurable via `-monitor-port`)
- Test server: 8081 (hardcoded in test_server/main.go)

## Key Functions and Locations

### Connection Handling
- `handleConnection()` main.go:575 - Initial connection processing and IP validation
- `handleHTTP()` main.go:620 - HTTP/HTTPS proxy logic
- `handleSocks5()` main.go:673 - SOCKS5 proxy implementation

### Monitoring System
- `addConnection()` main.go:96 - Register new connections
- `removeConnection()` main.go:118 - Clean up finished connections
- `broadcastUpdate()` main.go:152 - Send updates to WebSocket clients
- `handleWebSocket()` main.go:181 - WebSocket connection handler

### Configuration
- `loadConfig()` main.go:75 - Load and parse config.yaml
- `isPortAvailable()` main.go:519 - Check port availability

## Dependencies

- `github.com/gorilla/websocket` - WebSocket support for real-time monitoring
- `gopkg.in/yaml.v2` - YAML configuration parsing
- Standard Go libraries for networking and HTTP

## Development Notes

### Adding New Features
- Connection tracking uses thread-safe maps with RWMutex
- WebSocket updates are batched to prevent flooding
- All monitoring data is JSON-serializable

### Testing Strategy
- Use `test.sh` for comprehensive automated testing
- Test server provides simple HTTP responses for validation
- Both HTTP and SOCKS5 protocols should be tested
- Monitor dashboard functionality with WebSocket connections

### Port Management
- Check port availability before starting services
- Proxy and monitoring servers run on separate ports
- Test server runs independently on port 8081