package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

var (
	debugMode   bool
	allowedNets []*net.IPNet
)

func main() {
	flag.BoolVar(&debugMode, "debug", false, "Debug mode")
	flag.Parse()
	if !debugMode {
		log.SetOutput(io.Discard)
	}

	// Load config
	data, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}

	var cfg struct {
		Port int      `json:"port"`
		IPv4 []string `json:"ipv4"`
		IPv6 []string `json:"ipv6"`
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatal(err)
	}

	port := cfg.Port
	if port == 0 {
		port = 1080
	}

	// Parse networks once at startup
	for _, cidr := range append(cfg.IPv4, cfg.IPv6...) {
		if _, network, err := net.ParseCIDR(cidr); err == nil {
			allowedNets = append(allowedNets, network)
		}
	}

	// Start server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("SOCKS5 on 127.0.0.1:%d\n", port)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handle(clientConn)
	}
}

func handle(clientConn net.Conn) {
	defer clientConn.Close()

	// Check client IP
	clientIP := clientConn.RemoteAddr().(*net.TCPAddr).IP
	for _, network := range allowedNets {
		if network.Contains(clientIP) {
			goto allowed
		}
	}
	if debugMode {
		log.Printf("Blocked %s", clientIP)
	}
	return

allowed:
	// Auth phase
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(clientConn, buffer[:2]); err != nil || buffer[0] != 0x05 || buffer[1] == 0 {
		return
	}

	if _, err := io.ReadFull(clientConn, make([]byte, buffer[1])); err != nil {
		return
	}

	clientConn.Write([]byte{0x05, 0x00})

	// Request phase
	if _, err := io.ReadFull(clientConn, buffer[:4]); err != nil || buffer[1] != 0x01 {
		return
	}

	// Get destination
	var hostName string
	switch buffer[3] {
	case 0x01: // IPv4
		ipBytes := make([]byte, 4)
		if _, err := io.ReadFull(clientConn, ipBytes); err != nil {
			return
		}
		hostName = net.IP(ipBytes).String()
	case 0x03: // Domain
		lengthByte := make([]byte, 1)
		if _, err := io.ReadFull(clientConn, lengthByte); err != nil {
			return
		}
		domainBytes := make([]byte, lengthByte[0])
		if _, err := io.ReadFull(clientConn, domainBytes); err != nil {
			return
		}
		hostName = string(domainBytes)
	default:
		return
	}

	// Get port
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(clientConn, portBytes); err != nil {
		return
	}
	destPort := int(portBytes[0])<<8 | int(portBytes[1])

	// Connect
	targetConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", hostName, destPort))
	if err != nil {
		clientConn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer targetConn.Close()

	// Success
	clientConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	if debugMode {
		log.Printf("Connected to %s:%d", hostName, destPort)
	}

	// Proxy data
	go io.Copy(targetConn, clientConn)
	io.Copy(clientConn, targetConn)
}
