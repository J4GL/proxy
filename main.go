package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

const (
	proxyPort     = "8080"
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

var debugMode bool

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
	flag.Parse()

	if !isPortAvailable(proxyPort) {
		log.Fatalf("Port %s is already in use.", proxyPort)
	}

	allowedIPs, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	listener, err := net.Listen("tcp", ":"+proxyPort)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", proxyPort, err)
	}
	defer listener.Close()
	log.Printf("Proxy server listening on port %s", proxyPort)

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

	reader := bufio.NewReader(conn)
	firstByte, err := reader.Peek(1)
	if err != nil {
		return
	}

	if firstByte[0] == socks5Version {
		if debug {
			log.Println("Detected SOCKS5 connection")
		}
		handleSocks5(conn, reader, debug)
	} else {
		if debug {
			log.Println("Detected HTTP connection")
		}
		handleHTTP(conn, reader, debug)
	}
}

func handleHTTP(clientConn net.Conn, reader *bufio.Reader, debug bool) {
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

func handleSocks5(clientConn net.Conn, reader *bufio.Reader, debug bool) {
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