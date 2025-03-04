package main

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestSOCKS5Proxy(t *testing.T) {
	// Start a test server
	go main()

	// Wait for the server to start
	time.Sleep(time.Second)

	// Connect to the proxy server
	conn, err := net.Dial("tcp", "127.0.0.1:1080")
	if err != nil {
		t.Fatal("Failed to connect to proxy server:", err)
	}
	defer conn.Close()

	// Send SOCKS5 handshake
	_, err = conn.Write([]byte{0x05, 0x01, 0x00})
	if err != nil {
		t.Fatal("Failed to send handshake:", err)
	}

	// Read handshake response
	response := make([]byte, 2)
	_, err = conn.Read(response)
	if err != nil {
		t.Fatal("Failed to read handshake response:", err)
	}

	// Verify handshake response
	expectedResponse := []byte{0x05, 0x00}
	if !bytes.Equal(response, expectedResponse) {
		t.Fatalf("Unexpected handshake response. Got %v, want %v", response, expectedResponse)
	}

	// Send connection request (connect to example.com:80)
	request := []byte{0x05, 0x01, 0x00, 0x03, 0x0b}
	request = append(request, []byte("example.com")...)
	request = append(request, 0x00, 0x50) // Port 80
	_, err = conn.Write(request)
	if err != nil {
		t.Fatal("Failed to send connection request:", err)
	}

	// Read connection response
	response = make([]byte, 10)
	_, err = conn.Read(response)
	if err != nil {
		t.Fatal("Failed to read connection response:", err)
	}

	// Verify connection response (success)
	expectedResponse = []byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(response, expectedResponse) {
		t.Fatalf("Unexpected connection response. Got %v, want %v", response, expectedResponse)
	}
}