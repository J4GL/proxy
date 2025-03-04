package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
)

type Config struct {
	Port int      `json:"port"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

func isIPAllowed(ip net.IP) bool {
	data, err := os.ReadFile("config.json")
	if err != nil {
		debugLog("Failed to read allowed IPs: %v", err)
		return false
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		debugLog("Failed to parse allowed IPs: %v", err)
		return false
	}

	for _, cidr := range append(config.IPv4, config.IPv6...) {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

var debugMode bool

func init() {
	flag.BoolVar(&debugMode, "debug", false, "Enable debug mode with verbose logging")
}

func debugLog(format string, v ...interface{}) {
	if debugMode {
		log.Printf(format, v...)
	}
}

func main() {
	flag.Parse()

	// Configure logging based on debug mode
	if !debugMode {
		log.SetOutput(ioutil.Discard)
	}

	// Read configuration file
	data, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal("Failed to read config file:", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatal("Failed to parse config file:", err)
	}

	// Use default port if not specified
	if config.Port == 0 {
		config.Port = 1080
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		log.Fatal("Failed to start server:", err)
	}
	defer listener.Close()

	// Always show the startup message
	fmt.Printf("SOCKS5 proxy server listening on :%d\n", config.Port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Failed to accept connection:", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(client net.Conn) {
	defer client.Close()

	// Check if client IP is allowed
	ip := net.ParseIP(client.RemoteAddr().(*net.TCPAddr).IP.String())
	if !isIPAllowed(ip) {
		debugLog("Connection rejected from unauthorized IP: %s", ip)
		return
	}

	// Read the SOCKS5 version and number of authentication methods
	buf := make([]byte, 2)
	_, err := io.ReadFull(client, buf)
	if err != nil {
		debugLog("Failed to read SOCKS5 header: %v", err)
		return
	}

	// We only support SOCKS5 version and require at least one authentication method
	if buf[0] != 0x05 || buf[1] == 0 {
		debugLog("Unsupported SOCKS5 version or no authentication methods")
		return
	}

	// Read authentication methods
	authMethods := make([]byte, int(buf[1]))
	_, err = io.ReadFull(client, authMethods)
	if err != nil {
		debugLog("Failed to read authentication methods: %v", err)
		return
	}

	// Respond with no authentication required
	_, err = client.Write([]byte{0x05, 0x00})
	if err != nil {
		debugLog("Failed to send authentication response: %v", err)
		return
	}

	// Read the SOCKS5 request
	buf = make([]byte, 4)
	_, err = io.ReadFull(client, buf)
	if err != nil {
		debugLog("Failed to read SOCKS5 request: %v", err)
		return
	}

	// We only support CONNECT method (0x01)
	if buf[1] != 0x01 {
		debugLog("Unsupported SOCKS5 command")
		return
	}

	// Read the destination address
	var dstAddr string
	switch buf[3] {
	case 0x01: // IPv4
		addr := make([]byte, 4)
		_, err = io.ReadFull(client, addr)
		if err != nil {
			debugLog("Failed to read IPv4 address: %v", err)
			return
		}
		dstAddr = net.IP(addr).String()
	case 0x03: // Domain name
		addrLen := make([]byte, 1)
		_, err = io.ReadFull(client, addrLen)
		if err != nil {
			debugLog("Failed to read domain name length: %v", err)
			return
		}
		addr := make([]byte, addrLen[0])
		_, err = io.ReadFull(client, addr)
		if err != nil {
			debugLog("Failed to read domain name: %v", err)
			return
		}
		dstAddr = string(addr)
	default:
		debugLog("Unsupported address type")
		return
	}

	// Read the destination port
	port := make([]byte, 2)
	_, err = io.ReadFull(client, port)
	if err != nil {
		debugLog("Failed to read destination port: %v", err)
		return
	}
	dstPort := int(port[0])<<8 | int(port[1])

	// Connect to the destination
	dstConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", dstAddr, dstPort))
	if err != nil {
		debugLog("Failed to connect to destination: %v", err)
		client.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer dstConn.Close()

	// Send success response
	_, err = client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	if err != nil {
		debugLog("Failed to send success response: %v", err)
		return
	}

	debugLog("Client connected successfully to %s:%d", dstAddr, dstPort)

	// Start proxying data
	go func() {
		_, err := io.Copy(dstConn, client)
		if err != nil {
			debugLog("Error while copying client to destination: %v", err)
		}
	}()

	_, err = io.Copy(client, dstConn)
	if err != nil {
		debugLog("Error while copying destination to client: %v", err)
	}
}
