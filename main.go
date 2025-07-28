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
	ID            string    `json:"id"`
	ClientIP      string    `json:"client_ip"`
	Protocol      string    `json:"protocol"`
	Destination   string    `json:"destination"`
	DomainName    string    `json:"domain_name"`
	StartTime     time.Time `json:"start_time"`
	Duration      string    `json:"duration"`
	BytesReceived int64     `json:"bytes_received"`
	BytesSent     int64     `json:"bytes_sent"`
	BandwidthIn   float64   `json:"bandwidth_in"`  // bytes per second (current window)
	BandwidthOut  float64   `json:"bandwidth_out"` // bytes per second (current window)
	// For time-windowed bandwidth calculation
	LastUpdateTime  time.Time `json:"-"`
	WindowBytesIn   int64     `json:"-"`
	WindowBytesOut  int64     `json:"-"`
	WindowStartTime time.Time `json:"-"`
}

// MonitoringStats holds overall statistics
type MonitoringStats struct {
	TotalConnections    int                        `json:"total_connections"`
	ActiveConnections   map[string]*ConnectionInfo `json:"active_connections"`
	TotalBytesReceived  int64                      `json:"total_bytes_received"`
	TotalBytesSent      int64                      `json:"total_bytes_sent"`
	CurrentBandwidthIn  float64                    `json:"current_bandwidth_in"`  // bytes per second
	CurrentBandwidthOut float64                    `json:"current_bandwidth_out"` // bytes per second
	mutex               sync.RWMutex
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
		ID:              id,
		ClientIP:        clientIP,
		Protocol:        protocol,
		Destination:     destination,
		DomainName:      domainName,
		StartTime:       time.Now(),
		BytesReceived:   0,
		BytesSent:       0,
		BandwidthIn:     0,
		BandwidthOut:    0,
		WindowBytesIn:   0,
		WindowBytesOut:  0,
		WindowStartTime: time.Time{},
		LastUpdateTime:  time.Time{},
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

// updateBandwidth updates bandwidth statistics for a connection using a time window
func updateBandwidth(id string, bytesReceived, bytesSent int64) {
	stats.mutex.Lock()
	defer stats.mutex.Unlock()

	if conn, exists := stats.ActiveConnections[id]; exists {
		now := time.Now()

		// Update total bytes
		conn.BytesReceived += bytesReceived
		conn.BytesSent += bytesSent
		stats.TotalBytesReceived += bytesReceived
		stats.TotalBytesSent += bytesSent

		// Initialize window if this is the first update
		if conn.WindowStartTime.IsZero() {
			conn.WindowStartTime = now
			conn.LastUpdateTime = now
			conn.WindowBytesIn = 0
			conn.WindowBytesOut = 0
		}

		// Add bytes to current window
		conn.WindowBytesIn += bytesReceived
		conn.WindowBytesOut += bytesSent
		conn.LastUpdateTime = now

		// Calculate bandwidth over the current window (minimum 1 second)
		windowDuration := now.Sub(conn.WindowStartTime).Seconds()
		if windowDuration >= 1.0 {
			conn.BandwidthIn = float64(conn.WindowBytesIn) / windowDuration
			conn.BandwidthOut = float64(conn.WindowBytesOut) / windowDuration

			// Reset window
			conn.WindowStartTime = now
			conn.WindowBytesIn = 0
			conn.WindowBytesOut = 0
		} else if windowDuration > 0 {
			// For very short windows, show instantaneous rate
			conn.BandwidthIn = float64(conn.WindowBytesIn) / windowDuration
			conn.BandwidthOut = float64(conn.WindowBytesOut) / windowDuration
		}
	}

	// Signal broadcast update (non-blocking)
	select {
	case broadcastChan <- struct{}{}:
	default:
		// Channel is full, skip this update to prevent blocking
	}
}

// copyWithTracking copies data between connections while tracking bandwidth
func copyWithTracking(dst io.Writer, src io.Reader, connID string, isOutbound bool) (written int64, err error) {
	buffer := make([]byte, 32*1024) // 32KB buffer
	for {
		nr, er := src.Read(buffer)
		if nr > 0 {
			nw, ew := dst.Write(buffer[0:nr])
			if nw > 0 {
				written += int64(nw)
				// Update bandwidth tracking
				if isOutbound {
					updateBandwidth(connID, 0, int64(nw))
				} else {
					updateBandwidth(connID, int64(nw), 0)
				}
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return written, err
}

// getStats returns current statistics (thread-safe)
func getStats() MonitoringStats {
	stats.mutex.RLock()
	defer stats.mutex.RUnlock()

	now := time.Now()
	result := MonitoringStats{
		TotalConnections:   stats.TotalConnections,
		ActiveConnections:  make(map[string]*ConnectionInfo),
		TotalBytesReceived: stats.TotalBytesReceived,
		TotalBytesSent:     stats.TotalBytesSent,
	}

	var totalBandwidthIn, totalBandwidthOut float64
	for id, conn := range stats.ActiveConnections {
		connCopy := *conn
		connCopy.Duration = time.Since(conn.StartTime).Round(time.Second).String()

		// Check if connection has been idle for more than 2 seconds
		if !conn.LastUpdateTime.IsZero() && now.Sub(conn.LastUpdateTime) > 2*time.Second {
			// Connection is idle, bandwidth should be 0
			connCopy.BandwidthIn = 0
			connCopy.BandwidthOut = 0
		}

		result.ActiveConnections[id] = &connCopy

		// Only accumulate bandwidth for non-idle connections
		totalBandwidthIn += connCopy.BandwidthIn
		totalBandwidthOut += connCopy.BandwidthOut
	}

	result.CurrentBandwidthIn = totalBandwidthIn
	result.CurrentBandwidthOut = totalBandwidthOut
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
	http.ServeFile(w, r, "dashboard.html")
}

// startMonitoringServer starts the web monitoring server
func startMonitoringServer(port string) {
	// Create a new ServeMux to avoid conflicts with default handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleDashboard)
	mux.HandleFunc("/ws", handleWebSocket)
	mux.HandleFunc("/api/stats", handleAPI)
	
	// Serve static files (CSS and JS)
	fs := http.FileServer(http.Dir("static/"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

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
		lastBroadcast := time.Time{}
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		pendingUpdate := false

		for {
			select {
			case <-broadcastChan:
				// Mark that we have a pending update
				pendingUpdate = true

				// If enough time has passed since last broadcast, send immediately
				if time.Since(lastBroadcast) >= 1*time.Second {
					broadcastUpdate()
					lastBroadcast = time.Now()
					pendingUpdate = false
				}

			case <-ticker.C:
				// Send update if we have pending changes
				if pendingUpdate {
					broadcastUpdate()
					lastBroadcast = time.Now()
					pendingUpdate = false
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

	// Use tracking copies for bandwidth monitoring
	go copyWithTracking(serverConn, clientConn, connID, true) // Client to server (outbound)
	copyWithTracking(clientConn, serverConn, connID, false)   // Server to client (inbound)
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

	// Use tracking copies for bandwidth monitoring
	go copyWithTracking(destConn, reader, connID, true)   // Client to server (outbound)
	copyWithTracking(clientConn, destConn, connID, false) // Server to client (inbound)
}
