package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
)

var (
	debugMode   bool
	allowedNets []*net.IPNet
	config      struct {
		Port int      `json:"port"`
		IPv4 []string `json:"ipv4"`
		IPv6 []string `json:"ipv6"`
	}
)

const (
	defaultPort = 1080
	socks5Ver   = 0x05
	connectCmd  = 0x01
	ipv4Type    = 0x01
	domainType  = 0x03
)

func init() {
	flag.BoolVar(&debugMode, "debug", false, "Debug mode")
	flag.Parse()
	
	if !debugMode {
		log.SetOutput(io.Discard)
	}

	// Load and parse config once
	data, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatal(err)
	}

	// Pre-allocate slice capacity
	allowedNets = make([]*net.IPNet, 0, len(config.IPv4)+len(config.IPv6))
	for _, cidr := range append(config.IPv4, config.IPv6...) {
		if _, network, err := net.ParseCIDR(cidr); err == nil {
			allowedNets = append(allowedNets, network)
		}
	}
}

func main() {
	port := config.Port
	if port == 0 {
		port = defaultPort
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

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

	// Buffer pool to reduce allocations
	var bufPool sync.Pool
	bufPool.New = func() interface{} {
		return make([]byte, 256)
	}

	// Check client IP
	clientIP := clientConn.RemoteAddr().(*net.TCPAddr).IP
	if !isAllowedIP(clientIP) {
		if debugMode {
			log.Printf("Blocked %s", clientIP)
		}
		return
	}

	// Auth phase
	buf := bufPool.Get().([]byte)
	defer bufPool.Put(buf)

	if err := readAndVerify(clientConn, buf[:2], func(b []byte) bool {
		return b[0] == socks5Ver && b[1] != 0
	}); err != nil {
		return
	}

	if _, err := io.ReadFull(clientConn, buf[:buf[1]]); err != nil {
		return
	}

	if _, err := clientConn.Write([]byte{socks5Ver, 0x00}); err != nil {
		return
	}

	// Request phase
	if err := readAndVerify(clientConn, buf[:4], func(b []byte) bool {
		return b[1] == connectCmd
	}); err != nil {
		return
	}

	// Get destination
	hostName, err := parseDestination(clientConn, buf, buf[3])
	if err != nil {
		return
	}

	// Get port
	if _, err := io.ReadFull(clientConn, buf[:2]); err != nil {
		return
	}
	destPort := int(buf[0])<<8 | int(buf[1])

	// Connect
	targetConn, err := net.Dial("tcp", net.JoinHostPort(hostName, fmt.Sprintf("%d", destPort)))
	if err != nil {
		clientConn.Write([]byte{socks5Ver, 0x01, 0x00, ipv4Type, 0, 0, 0, 0, 0, 0})
		return
	}
	defer targetConn.Close()

	// Success response
	if _, err := clientConn.Write([]byte{socks5Ver, 0x00, 0x00, ipv4Type, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	if debugMode {
		log.Printf("Connected to %s:%d", hostName, destPort)
	}

	// Proxy data with wait group
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(targetConn, clientConn)
		targetConn.Close()
	}()
	go func() {
		defer wg.Done()
		io.Copy(clientConn, targetConn)
		clientConn.Close()
	}()
	wg.Wait()
}

func isAllowedIP(ip net.IP) bool {
	for _, network := range allowedNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func readAndVerify(conn net.Conn, buf []byte, verify func([]byte) bool) error {
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}
	if !verify(buf) {
		return fmt.Errorf("verification failed")
	}
	return nil
}

func parseDestination(conn net.Conn, buf []byte, addrType byte) (string, error) {
	switch addrType {
	case ipv4Type:
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return "", err
		}
		return net.IP(buf[:4]).String(), nil
	case domainType:
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return "", err
		}
		length := buf[0]
		if _, err := io.ReadFull(conn, buf[:length]); err != nil {
			return "", err
		}
		return string(buf[:length]), nil
	default:
		return "", fmt.Errorf("unsupported address type")
	}
}
