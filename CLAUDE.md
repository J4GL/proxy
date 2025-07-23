# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go-based multi-protocol proxy server that supports both HTTP/HTTPS and SOCKS5 protocols on a single port (8080). The server includes IP-based access control, real-time monitoring via WebSocket, and a web dashboard for connection tracking.

## Key Architecture

### Protocol Detection
- Uses protocol sniffing by peeking at the first byte of incoming connections (main.go:644-648)
- SOCKS5 connections start with `0x05`, all others treated as HTTP
- Single listener handles both protocols seamlessly using `bufio.NewReader.Peek(1)`

### Core Components
- **main.go**: Single-file implementation containing entire proxy server (800+ lines)
- **config.yaml**: IP whitelist configuration loaded via `gopkg.in/yaml.v2`
- **test_server/main.go**: Simple HTTP file server on port 8081 for testing
- **monitoring_test.go**: Comprehensive Go tests for monitoring functionality
- **test.sh**: Automated test suite handling server lifecycle and protocol testing

### Monitoring System Architecture
- **Thread-Safe Statistics**: Uses `sync.RWMutex` for concurrent access to connection data
- **WebSocket Broadcasting**: Dedicated goroutine with batching to prevent message flooding
- **Real-Time Dashboard**: Embedded HTML/CSS/JavaScript served directly from Go strings
- **REST API**: JSON endpoint at `/api/stats` for programmatic access
- **Connection Tracking**: Unique IDs generated from nanosecond timestamps + connection count
- **Reverse DNS**: Automatic domain name resolution for monitoring visibility

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
[project_root]/test.sh

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

### Connection Handling Flow
- `handleConnection()` main.go:617 - Main connection entry point with IP validation and protocol routing
- `handleHTTP()` main.go:662 - HTTP/HTTPS proxy logic with CONNECT method for HTTPS tunneling
- `handleSocks5()` main.go:715 - Complete SOCKS5 implementation (IPv4/IPv6/domain support)

### Monitoring System Components
- `addConnection()` main.go:128 - Register new connections with reverse DNS lookup
- `removeConnection()` main.go:154 - Thread-safe cleanup of finished connections
- `getStats()` main.go:168 - Thread-safe statistics retrieval with mutex locking
- `broadcastUpdate()` main.go:188 - WebSocket update broadcasting with batching
- `handleWebSocket()` main.go:217 - WebSocket connection lifecycle management
- `handleAPI()` main.go:250 - REST API endpoint serving JSON statistics
- `handleDashboard()` main.go:259 - Embedded HTML dashboard serving

### Configuration and Utilities
- `loadConfig()` main.go:76 - YAML configuration parsing with error handling
- `reverseDNSLookup()` main.go:97 - Domain name resolution for monitoring visibility
- `generateConnectionID()` main.go:554 - Unique ID generation using nanosecond timestamps
- `isPortAvailable()` main.go:559 - TCP port availability checking

## Dependencies

- `github.com/gorilla/websocket` - WebSocket support for real-time monitoring
- `gopkg.in/yaml.v2` - YAML configuration parsing
- Standard Go libraries for networking and HTTP

## Development Notes

### Architecture Patterns
- **Single-File Design**: Entire proxy server contained in main.go for simplicity
- **Protocol Agnostic**: Single port handles multiple protocols through byte inspection
- **Concurrent Safety**: All shared data protected by appropriate mutex types
- **Embedded Resources**: HTML dashboard included as Go string literals (lines 262-506)
- **Graceful Degradation**: Monitoring failures don't affect proxy functionality

### Connection Lifecycle Management
- Each connection gets unique ID from `generateConnectionID()` using nanosecond precision
- Connections tracked in thread-safe map with automatic cleanup via defer statements
- Reverse DNS lookup performed asynchronously to avoid blocking connection handling
- WebSocket broadcasts batched with 100ms delay to prevent client flooding

### Testing Strategy
- **Automated Suite**: `test.sh` handles complete server lifecycle (build, start, test, cleanup)
- **Protocol Coverage**: Tests both HTTP and SOCKS5 protocols with curl/wget
- **Monitoring Tests**: `monitoring_test.go` validates WebSocket functionality and API endpoints
- **Manual Testing**: README provides curl commands for quick verification
- **Error Simulation**: Test framework captures both stdout and stderr for debugging

### Adding New Features
- Monitor system uses `broadcastChan` (buffered channel) to signal updates without blocking
- All statistics operations use `stats.mutex.RLock()/RUnlock()` for read access
- WebSocket client management uses separate `wsMutex` to avoid deadlocks
- New protocol support would require modification of protocol detection logic (main.go:644-648)