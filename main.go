package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v2"
)

const (
	proxyPort     = "8080"
	monitorPort   = "8082"
	socks5Version = 0x05
	noAuth        = 0x00
	connectCmd    = 0x01
	ipv4Addr      = 0x01
	domainAddr    = 0x03
	ipv6Addr      = 0x04
)

// Config holds the structure of the YAML configuration file.
type Config struct {
	AllowedIPs []string `yaml:"allowed_ips"`
}

// ConnectionInfo holds information about an active connection
type ConnectionInfo struct {
	ID          string    `json:"id"`
	ClientIP    string    `json:"client_ip"`
	Protocol    string    `json:"protocol"`
	Destination string    `json:"destination"`
	DomainName  string    `json:"domain_name"`
	StartTime   time.Time `json:"start_time"`
	Duration    string    `json:"duration"`
}

// MonitoringStats holds overall statistics
type MonitoringStats struct {
	TotalConnections  int                        `json:"total_connections"`
	ActiveConnections map[string]*ConnectionInfo `json:"active_connections"`
	mutex             sync.RWMutex
}

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow connections from any origin
	},
}

var (
	debugMode      bool
	monitoringPort string
	stats          = &MonitoringStats{
		ActiveConnections: make(map[string]*ConnectionInfo),
	}
	wsClients     = make(map[*websocket.Conn]bool)
	wsMutex       sync.RWMutex
	broadcastChan = make(chan struct{}, 100) // Buffered channel to prevent blocking
)

// loadConfig reads the YAML config file and returns a map of allowed IPs for quick lookup.
func loadConfig(path string) (map[string]bool, error) {
	allowedIPs := make(map[string]bool)
	configFile, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read config file '%s': %v", path, err)
	}

	var config Config
	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		return nil, fmt.Errorf("could not parse config file: %v", err)
	}

	for _, ip := range config.AllowedIPs {
		allowedIPs[ip] = true
	}
	log.Printf("Loaded %d allowed IPs from config", len(allowedIPs))
	return allowedIPs, nil
}

// reverseDNSLookup attempts to resolve an IP address to a domain name
func reverseDNSLookup(destination string) string {
	// Parse the destination to extract just the IP if it contains a port
	host, _, err := net.SplitHostPort(destination)
	if err != nil {
		// If SplitHostPort fails, assume destination is just a host
		host = destination
	}

	// Check if the host is already a domain name (contains letters)
	if net.ParseIP(host) == nil {
		// It's already a domain name, return it
		return host
	}

	// Perform reverse DNS lookup with a timeout
	names, err := net.LookupAddr(host)
	if err != nil || len(names) == 0 {
		// If lookup fails or no names found, return the original IP
		return host
	}

	// Return the first domain name, removing trailing dot if present
	domainName := names[0]
	if strings.HasSuffix(domainName, ".") {
		domainName = domainName[:len(domainName)-1]
	}

	return domainName
}

// addConnection registers a new connection in the monitoring system
func addConnection(id, clientIP, protocol, destination string) {
	// Perform reverse DNS lookup for the destination
	domainName := reverseDNSLookup(destination)

	stats.mutex.Lock()
	conn := &ConnectionInfo{
		ID:          id,
		ClientIP:    clientIP,
		Protocol:    protocol,
		Destination: destination,
		DomainName:  domainName,
		StartTime:   time.Now(),
	}
	stats.ActiveConnections[id] = conn
	stats.TotalConnections++
	stats.mutex.Unlock()

	// Signal broadcast update (non-blocking)
	select {
	case broadcastChan <- struct{}{}:
	default:
		// Channel is full, skip this update to prevent blocking
	}
}

// removeConnection removes a connection from the monitoring system
func removeConnection(id string) {
	stats.mutex.Lock()
	delete(stats.ActiveConnections, id)
	stats.mutex.Unlock()

	// Signal broadcast update (non-blocking)
	select {
	case broadcastChan <- struct{}{}:
	default:
		// Channel is full, skip this update to prevent blocking
	}
}

// getStats returns current statistics (thread-safe)
func getStats() MonitoringStats {
	stats.mutex.RLock()
	defer stats.mutex.RUnlock()

	// Update durations for active connections
	result := MonitoringStats{
		TotalConnections:  stats.TotalConnections,
		ActiveConnections: make(map[string]*ConnectionInfo),
	}

	for id, conn := range stats.ActiveConnections {
		connCopy := *conn
		connCopy.Duration = time.Since(conn.StartTime).Round(time.Second).String()
		result.ActiveConnections[id] = &connCopy
	}

	return result
}

// broadcastUpdate sends current stats to all WebSocket clients
func broadcastUpdate() {
	currentStats := getStats()
	message, err := json.Marshal(currentStats)
	if err != nil {
		log.Printf("Error marshaling stats: %v", err)
		return
	}

	wsMutex.Lock()
	defer wsMutex.Unlock()

	// Create a list of clients to avoid holding the lock during writes
	clients := make([]*websocket.Conn, 0, len(wsClients))
	for client := range wsClients {
		clients = append(clients, client)
	}

	// Write to each client sequentially to avoid concurrent writes
	for _, client := range clients {
		err := client.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			log.Printf("Error sending WebSocket message: %v", err)
			client.Close()
			delete(wsClients, client)
		}
	}
}

// handleWebSocket handles WebSocket connections for real-time updates
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Add client to the list
	wsMutex.Lock()
	wsClients[conn] = true
	wsMutex.Unlock()

	// Send initial data
	currentStats := getStats()
	message, err := json.Marshal(currentStats)
	if err == nil {
		conn.WriteMessage(websocket.TextMessage, message)
	}

	// Keep connection alive and handle disconnection
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			wsMutex.Lock()
			delete(wsClients, conn)
			wsMutex.Unlock()
			break
		}
	}
}

// handleAPI handles REST API requests for monitoring data
func handleAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	currentStats := getStats()
	json.NewEncoder(w).Encode(currentStats)
}

// handleDashboard serves the monitoring dashboard HTML
func handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Proxy Server Monitor</title>
    <style>
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            margin: 0;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 20px;
            border-radius: 10px;
            margin-bottom: 20px;
            text-align: center;
        }
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        .stat-card {
            background: white;
            padding: 20px;
            border-radius: 10px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            text-align: center;
        }
        .stat-number {
            font-size: 2.5em;
            font-weight: bold;
            color: #667eea;
            margin-bottom: 10px;
        }
        .stat-label {
            color: #666;
            font-size: 1.1em;
        }
        .connections-table {
            background: white;
            border-radius: 10px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            overflow: hidden;
        }
        .table-header {
            background: #667eea;
            color: white;
            padding: 15px 20px;
            font-size: 1.2em;
            font-weight: bold;
        }
        table {
            width: 100%;
            border-collapse: collapse;
        }
        th, td {
            padding: 12px 15px;
            text-align: left;
            border-bottom: 1px solid #eee;
        }
        th {
            background-color: #f8f9fa;
            font-weight: 600;
            color: #333;
        }
        tr:hover {
            background-color: #f8f9fa;
        }
        .protocol-badge {
            padding: 4px 8px;
            border-radius: 4px;
            font-size: 0.8em;
            font-weight: bold;
        }
        .protocol-http {
            background-color: #e3f2fd;
            color: #1976d2;
        }
        .protocol-socks5 {
            background-color: #f3e5f5;
            color: #7b1fa2;
        }
        .status {
            margin-bottom: 20px;
            padding: 10px;
            background: #e8f5e8;
            border-left: 4px solid #4caf50;
            border-radius: 4px;
        }
        .no-connections {
            text-align: center;
            padding: 40px;
            color: #666;
            font-style: italic;
        }
        .last-updated {
            text-align: center;
            margin-top: 20px;
            color: #666;
            font-size: 0.9em;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🔍 Proxy Server Monitor</h1>
            <p>Real-time monitoring dashboard for proxy connections</p>
        </div>

        <div class="stats-grid">
            <div class="stat-card">
                <div class="stat-number" id="total-connections">0</div>
                <div class="stat-label">Total Connections</div>
            </div>
            <div class="stat-card">
                <div class="stat-number" id="active-connections">0</div>
                <div class="stat-label">Active Connections</div>
            </div>
        </div>

        <div class="status" id="connection-status">
            🟢 Connected to monitoring server
        </div>

        <div class="connections-table">
            <div class="table-header">Active Connections</div>
            <div id="connections-content">
                <div class="no-connections">No active connections</div>
            </div>
        </div>

        <div class="last-updated" id="last-updated">
            Last updated: Never
        </div>
    </div>

    <script>
        let ws;
        let reconnectInterval;

        function connectWebSocket() {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = protocol + '//' + window.location.host + '/ws';

            ws = new WebSocket(wsUrl);

            ws.onopen = function() {
                console.log('WebSocket connected');
                document.getElementById('connection-status').innerHTML = '🟢 Connected to monitoring server';
                document.getElementById('connection-status').style.background = '#e8f5e8';
                document.getElementById('connection-status').style.borderColor = '#4caf50';
                if (reconnectInterval) {
                    clearInterval(reconnectInterval);
                    reconnectInterval = null;
                }
            };

            ws.onmessage = function(event) {
                const data = JSON.parse(event.data);
                updateDashboard(data);
            };

            ws.onclose = function() {
                console.log('WebSocket disconnected');
                document.getElementById('connection-status').innerHTML = '🔴 Disconnected from monitoring server';
                document.getElementById('connection-status').style.background = '#ffebee';
                document.getElementById('connection-status').style.borderColor = '#f44336';

                // Attempt to reconnect every 5 seconds
                if (!reconnectInterval) {
                    reconnectInterval = setInterval(connectWebSocket, 5000);
                }
            };

            ws.onerror = function(error) {
                console.error('WebSocket error:', error);
            };
        }

        function updateDashboard(data) {
            document.getElementById('total-connections').textContent = data.total_connections || 0;
            document.getElementById('active-connections').textContent = Object.keys(data.active_connections || {}).length;

            const connectionsContent = document.getElementById('connections-content');
            const connections = data.active_connections || {};

            if (Object.keys(connections).length === 0) {
                connectionsContent.innerHTML = '<div class="no-connections">No active connections</div>';
            } else {
                let tableHTML = '<table><thead><tr><th>Client IP</th><th>Protocol</th><th>Destination</th><th>Domain</th><th>Duration</th><th>Start Time</th></tr></thead><tbody>';

                Object.values(connections).forEach(conn => {
                    const protocolClass = conn.protocol.toLowerCase() === 'http' ? 'protocol-http' : 'protocol-socks5';
                    const startTime = new Date(conn.start_time).toLocaleString();

                    // Format domain name display
                    const domainDisplay = conn.domain_name && conn.domain_name !== conn.destination.split(':')[0] 
                        ? conn.domain_name 
                        : '<em>N/A</em>';
                    
                    tableHTML += '<tr>' +
                        '<td>' + conn.client_ip + '</td>' +
                        '<td><span class="protocol-badge ' + protocolClass + '">' + conn.protocol + '</span></td>' +
                        '<td>' + conn.destination + '</td>' +
                        '<td>' + domainDisplay + '</td>' +
                        '<td>' + conn.duration + '</td>' +
                        '<td>' + startTime + '</td>' +
                        '</tr>';
                });

                tableHTML += '</tbody></table>';
                connectionsContent.innerHTML = tableHTML;
            }

            document.getElementById('last-updated').textContent = 'Last updated: ' + new Date().toLocaleString();
        }

        // Initialize WebSocket connection
        connectWebSocket();

        // Fallback: fetch data every 30 seconds if WebSocket fails
        setInterval(function() {
            if (ws.readyState !== WebSocket.OPEN) {
                fetch('/api/stats')
                    .then(response => response.json())
                    .then(data => updateDashboard(data))
                    .catch(error => console.error('Error fetching data:', error));
            }
        }, 30000);
    </script>
</body>
</html>`

	fmt.Fprint(w, html)
}

// startMonitoringServer starts the web monitoring server
func startMonitoringServer(port string) {
	// Create a new ServeMux to avoid conflicts with default handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleDashboard)
	mux.HandleFunc("/ws", handleWebSocket)
	mux.HandleFunc("/api/stats", handleAPI)

	log.Printf("Starting monitoring server on port %s", port)
	log.Printf("Dashboard available at: http://vps.j4.gl:%s", port)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Printf("Monitoring server error: %v", err)
		log.Printf("Monitoring dashboard will not be available")
	}
}

// startBroadcastWorker starts a goroutine that handles WebSocket broadcasts sequentially
func startBroadcastWorker() {
	go func() {
		for range broadcastChan {
			broadcastUpdate()
			// Add a small delay to batch rapid updates
			time.Sleep(100 * time.Millisecond)

			// Drain any additional signals that came in during the delay
		drainLoop:
			for {
				select {
				case <-broadcastChan:
					// Drain additional signals
				default:
					break drainLoop
				}
			}
		}
	}()
}

// generateConnectionID creates a unique connection ID
func generateConnectionID() string {
	return fmt.Sprintf("conn_%d_%d", time.Now().UnixNano(), len(stats.ActiveConnections))
}

// isPortAvailable checks if a TCP port is available.
func isPortAvailable(port string) bool {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func main() {
	flag.BoolVar(&debugMode, "debug", false, "Enable debug logging for connections")
	flag.BoolVar(&debugMode, "d", false, "Enable debug logging for connections (shorthand)")
	flag.StringVar(&monitoringPort, "monitor-port", monitorPort, "Port for the monitoring web interface")
	flag.StringVar(&monitoringPort, "m", monitorPort, "Port for the monitoring web interface (shorthand)")
	flag.Parse()

	// Check if proxy port is available
	if !isPortAvailable(proxyPort) {
		log.Fatalf("Port %s is already in use.", proxyPort)
	}

	// Check if monitoring port is available
	if !isPortAvailable(monitoringPort) {
		log.Fatalf("Monitoring port %s is already in use.", monitoringPort)
	}

	allowedIPs, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Start broadcast worker for WebSocket updates
	startBroadcastWorker()

	// Start monitoring server in a separate goroutine
	go startMonitoringServer(monitoringPort)

	listener, err := net.Listen("tcp", ":"+proxyPort)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", proxyPort, err)
	}
	defer listener.Close()
	log.Printf("Proxy server listening on port %s", proxyPort)
	log.Printf("HTTP/HTTPS proxy configuration: http://vps.j4.gl:%s", proxyPort)
	log.Printf("SOCKS5 proxy configuration: socks5://vps.j4.gl:%s", proxyPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if debugMode {
				log.Printf("Failed to accept connection: %v", err)
			}
			continue
		}
		go handleConnection(conn, allowedIPs, debugMode)
	}
}

func handleConnection(conn net.Conn, allowedIPs map[string]bool, debug bool) {
	defer conn.Close()

	clientIP, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		if debug {
			log.Printf("Could not get client IP: %v", err)
		}
		return
	}

	if !allowedIPs[clientIP] {
		if debug {
			log.Printf("Connection from unauthorized IP %s blocked.", clientIP)
		}
		return
	}

	if debug {
		log.Printf("Accepted new client from %s", conn.RemoteAddr())
		log.Printf("Client %s is authorized.", clientIP)
	}

	// Generate unique connection ID
	connID := generateConnectionID()

	reader := bufio.NewReader(conn)
	firstByte, err := reader.Peek(1)
	if err != nil {
		return
	}

	if firstByte[0] == socks5Version {
		if debug {
			log.Println("Detected SOCKS5 connection")
		}
		handleSocks5(conn, reader, debug, connID, clientIP)
	} else {
		if debug {
			log.Println("Detected HTTP connection")
		}
		handleHTTP(conn, reader, debug, connID, clientIP)
	}
}

func handleHTTP(clientConn net.Conn, reader *bufio.Reader, debug bool, connID, clientIP string) {
	req, err := http.ReadRequest(reader)
	if err != nil {
		if debug {
			log.Printf("Failed to read HTTP request: %v", err)
		}
		return
	}

	address := req.Host
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "80")
	}

	// Register connection in monitoring system
	addConnection(connID, clientIP, "HTTP", address)
	defer removeConnection(connID)

	serverConn, err := net.Dial("tcp", address)
	if err != nil {
		if debug {
			log.Printf("Failed to connect to destination '%s': %v", address, err)
		}
		resp := &http.Response{
			StatusCode: http.StatusBadGateway,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Body:       io.NopCloser(strings.NewReader("Bad Gateway")),
		}
		resp.Write(clientConn)
		return
	}
	defer serverConn.Close()

	if req.Method == "CONNECT" {
		fmt.Fprint(clientConn, "HTTP/1.1 200 Connection established\r\n\r\n")
	} else {
		err = req.Write(serverConn)
		if err != nil {
			if debug {
				log.Printf("Failed to write request to destination: %v", err)
			}
			return
		}
	}

	if debug {
		log.Printf("Relaying data between client and %s", address)
	}
	go io.Copy(serverConn, clientConn)
	io.Copy(clientConn, serverConn)
}

func handleSocks5(clientConn net.Conn, reader *bufio.Reader, debug bool, connID, clientIP string) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		if debug {
			log.Printf("SOCKS5: Failed to read handshake: %v", err)
		}
		return
	}

	version := header[0]
	nMethods := header[1]

	if version != socks5Version {
		if debug {
			log.Printf("SOCKS5: Unsupported version: %d", version)
		}
		return
	}

	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(reader, methods); err != nil {
		if debug {
			log.Printf("SOCKS5: Failed to read methods: %v", err)
		}
		return
	}

	clientConn.Write([]byte{socks5Version, noAuth})

	reqHeader := make([]byte, 4)
	if _, err := io.ReadFull(reader, reqHeader); err != nil {
		if debug {
			log.Printf("SOCKS5: Failed to read request header: %v", err)
		}
		return
	}

	if reqHeader[0] != socks5Version || reqHeader[1] != connectCmd {
		if debug {
			log.Printf("SOCKS5: Invalid request. Version: %d, Command: %d", reqHeader[0], reqHeader[1])
		}
		return
	}

	var host string
	addrType := reqHeader[3]
	switch addrType {
	case ipv4Addr:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(reader, addr); err != nil {
			if debug {
				log.Printf("SOCKS5: Failed to read IPv4 address: %v", err)
			}
			return
		}
		host = net.IP(addr).String()
	case domainAddr:
		lenByte, err := reader.ReadByte()
		if err != nil {
			if debug {
				log.Printf("SOCKS5: Failed to read domain length: %v", err)
			}
			return
		}
		domain := make([]byte, lenByte)
		if _, err := io.ReadFull(reader, domain); err != nil {
			if debug {
				log.Printf("SOCKS5: Failed to read domain: %v", err)
			}
			return
		}
		host = string(domain)
	case ipv6Addr:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(reader, addr); err != nil {
			if debug {
				log.Printf("SOCKS5: Failed to read IPv6 address: %v", err)
			}
			return
		}
		host = net.IP(addr).String()
	default:
		if debug {
			log.Printf("SOCKS5: Unknown address type: %d", addrType)
		}
		return
	}

	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		if debug {
			log.Printf("SOCKS5: Failed to read port: %v", err)
		}
		return
	}
	port := binary.BigEndian.Uint16(portBytes)
	address := net.JoinHostPort(host, strconv.Itoa(int(port)))

	// Register connection in monitoring system
	addConnection(connID, clientIP, "SOCKS5", address)
	defer removeConnection(connID)

	destConn, err := net.Dial("tcp", address)
	if err != nil {
		if debug {
			log.Printf("SOCKS5: Failed to connect to destination '%s': %v", address, err)
		}
		clientConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}) // Host unreachable
		return
	}
	defer destConn.Close()

	clientConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})

	if debug {
		log.Printf("SOCKS5: Relaying data for %s", address)
	}
	go io.Copy(destConn, reader)
	io.Copy(clientConn, destConn)
}
