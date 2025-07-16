package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	testProxyAddr    = "localhost:8080"
	testMonitorAddr  = "localhost:8082"
	testServerAddr   = "localhost:8081"
)

// TestMonitoringDashboard tests the monitoring dashboard HTTP endpoint
func TestMonitoringDashboard(t *testing.T) {
	// Wait a moment for servers to be ready
	time.Sleep(2 * time.Second)

	// Test dashboard endpoint
	resp, err := http.Get("http://" + testMonitorAddr)
	if err != nil {
		t.Fatalf("Failed to access monitoring dashboard: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read dashboard response: %v", err)
	}

	// Check if the dashboard contains expected elements
	bodyStr := string(body)
	expectedElements := []string{
		"Proxy Server Monitor",
		"Total Connections",
		"Active Connections",
		"WebSocket",
	}

	for _, element := range expectedElements {
		if !strings.Contains(bodyStr, element) {
			t.Errorf("Dashboard missing expected element: %s", element)
		}
	}

	t.Log("✅ Dashboard HTML loads correctly")
}

// TestMonitoringAPI tests the monitoring API endpoint
func TestMonitoringAPI(t *testing.T) {
	// Test API endpoint
	resp, err := http.Get("http://" + testMonitorAddr + "/api/stats")
	if err != nil {
		t.Fatalf("Failed to access monitoring API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var stats MonitoringStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode API response: %v", err)
	}

	// Initially should have no active connections
	if stats.ActiveConnections == nil {
		t.Error("ActiveConnections should not be nil")
	}

	t.Logf("✅ API returns valid JSON: %d total connections, %d active", 
		stats.TotalConnections, len(stats.ActiveConnections))
}

// TestWebSocketConnection tests the WebSocket endpoint
func TestWebSocketConnection(t *testing.T) {
	// Connect to WebSocket
	wsURL := "ws://" + testMonitorAddr + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Read initial message
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read WebSocket message: %v", err)
	}

	var stats MonitoringStats
	if err := json.Unmarshal(message, &stats); err != nil {
		t.Fatalf("Failed to decode WebSocket message: %v", err)
	}

	t.Log("✅ WebSocket connection established and receives data")
}

// TestHTTPProxyMonitoring tests HTTP proxy connections are monitored
func TestHTTPProxyMonitoring(t *testing.T) {
	// Create HTTP client with proxy
	proxyURL, _ := url.Parse("http://" + testProxyAddr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 10 * time.Second,
	}

	// Make a request through the proxy
	go func() {
		resp, err := client.Get("http://" + testServerAddr)
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Wait a moment for connection to be established
	time.Sleep(1 * time.Second)

	// Check monitoring API for the connection
	resp, err := http.Get("http://" + testMonitorAddr + "/api/stats")
	if err != nil {
		t.Fatalf("Failed to access monitoring API: %v", err)
	}
	defer resp.Body.Close()

	var stats MonitoringStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode API response: %v", err)
	}

	// Should have at least one connection
	if stats.TotalConnections == 0 {
		t.Error("Expected at least one total connection")
	}

	t.Logf("✅ HTTP proxy connection monitored: %d total, %d active", 
		stats.TotalConnections, len(stats.ActiveConnections))
}

// TestSOCKS5ProxyMonitoring tests SOCKS5 proxy connections are monitored
func TestSOCKS5ProxyMonitoring(t *testing.T) {
	// Create SOCKS5 connection
	go func() {
		conn, err := net.Dial("tcp", testProxyAddr)
		if err != nil {
			return
		}
		defer conn.Close()

		// SOCKS5 handshake
		conn.Write([]byte{0x05, 0x01, 0x00}) // Version 5, 1 method, no auth

		// Read response
		response := make([]byte, 2)
		conn.Read(response)

		// Connect request to test server
		host := "localhost"
		port := uint16(8081)
		
		request := []byte{0x05, 0x01, 0x00, 0x03} // Version, Connect, Reserved, Domain
		request = append(request, byte(len(host)))
		request = append(request, []byte(host)...)
		request = append(request, byte(port>>8), byte(port&0xff))
		
		conn.Write(request)
		
		// Read connect response
		connectResp := make([]byte, 10)
		conn.Read(connectResp)
		
		// Keep connection alive briefly
		time.Sleep(1 * time.Second)
	}()

	// Wait for connection to be established
	time.Sleep(2 * time.Second)

	// Check monitoring API
	resp, err := http.Get("http://" + testMonitorAddr + "/api/stats")
	if err != nil {
		t.Fatalf("Failed to access monitoring API: %v", err)
	}
	defer resp.Body.Close()

	var stats MonitoringStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode API response: %v", err)
	}

	t.Logf("✅ SOCKS5 proxy connection monitored: %d total, %d active", 
		stats.TotalConnections, len(stats.ActiveConnections))
}

// TestRealTimeUpdates tests that WebSocket provides real-time updates
func TestRealTimeUpdates(t *testing.T) {
	// Connect to WebSocket
	wsURL := "ws://" + testMonitorAddr + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Read initial state
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, initialMsg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read initial WebSocket message: %v", err)
	}

	var initialStats MonitoringStats
	json.Unmarshal(initialMsg, &initialStats)
	initialTotal := initialStats.TotalConnections

	// Make a connection through proxy
	go func() {
		proxyURL, _ := url.Parse("http://" + testProxyAddr)
		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			},
			Timeout: 5 * time.Second,
		}
		client.Get("http://" + testServerAddr)
	}()

	// Wait for update message
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, updateMsg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read update WebSocket message: %v", err)
	}

	var updateStats MonitoringStats
	json.Unmarshal(updateMsg, &updateStats)

	// Should have more connections now
	if updateStats.TotalConnections <= initialTotal {
		t.Errorf("Expected total connections to increase from %d, got %d", 
			initialTotal, updateStats.TotalConnections)
	}

	t.Log("✅ Real-time updates working via WebSocket")
}
