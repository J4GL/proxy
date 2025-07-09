# Go Multi-Protocol Proxy Server

This project implements a multi-protocol proxy server in Go that can handle both HTTP/HTTPS and SOCKS5 connections on a single port. It's designed to be lightweight and efficient.

## How It Works

The proxy server listens for TCP connections on a single port (`8080` by default). When a new client connects, the server employs a protocol sniffing technique to determine whether the incoming request is for HTTP or SOCKS5.

1.  **Protocol Sniffing**: The server reads the very first byte of the incoming data stream without consuming it.
    *   If the first byte is `0x05`, it signifies the start of a SOCKS5 protocol handshake.
    *   For any other byte, the connection is assumed to be an HTTP request.

2.  **HTTP/HTTPS Handling (`handleHttp`)**:
    *   If the request is identified as HTTP, the server reads the full request using Go's standard `net/http` package.
    *   **For HTTPS (`CONNECT` method)**: When a client sends a `CONNECT` request, the proxy establishes a direct TCP tunnel. It connects to the destination server requested by the client, sends a `200 OK` response back to the client, and then blindly relays TCP data in both directions. This allows for secure end-to-end TLS communication.
    *   **For HTTP (e.g., `GET`, `POST`)**: The proxy parses the request, connects to the destination host, forwards the client's request, and then relays the server's response back to the client.

3.  **SOCKS5 Handling (`handleSocks5`)**:
    *   **Handshake**: The server performs the SOCKS5 handshake. It currently supports the "No Authentication Required" method (`0x00`).
    *   **Request**: It reads the client's request, which specifies the command (only `CONNECT` is supported) and the destination address (which can be an IPv4, IPv6, or domain name).
    *   **Connection**: The proxy connects to the requested destination address and port.
    *   **Relay**: If the connection is successful, it sends a success reply to the client and then begins relaying TCP data back and forth between the client and the destination server.

## How to Run

### Prerequisites

*   Go (version 1.18 or later)
*   `curl` and `wget` for testing

### 1. Build the Applications

First, compile the proxy server and the accompanying test server. Run these commands from the project's root directory:

```bash
# Build the proxy server
go build -o proxy_app main.go

# Build the test server
go build -o test_server/test_server_app test_server/main.go
```

### 2. Run the Servers

You need to run two applications in separate terminal windows: the test server and the proxy server.

**Terminal 1: Start the Test Server**

The test server is a simple file server that serves the content of `test.txt` on port `8081`.

```bash
# Navigate to the test_server directory
cd test_server

# Run the test server
./test_server_app
```
You should see a log message indicating the server is running and serving files from the current directory.

**Terminal 2: Start the Proxy Server**

The proxy server will listen on port `8080`.

```bash
# From the project root directory
./proxy_app
```
You will see a log message that the proxy is listening on port 8080.

### 3. Test the Proxy

You can now use `curl` and `wget` to make requests through the proxy to the test server.

**Test 1: curl with HTTP Proxy**

```bash
curl --proxy http://localhost:8080 http://localhost:8081/test.txt
```

**Test 2: curl with SOCKS5 Proxy**

```bash
curl --proxy socks5://localhost:8080 http://localhost:8081/test.txt
```

**Test 3: wget with HTTP Proxy**

```bash
wget -e "http_proxy=http://localhost:8080" http://localhost:8081/test.txt -O -
```

**Test 4: wget with SOCKS5 Proxy**

Note: `wget` uses the `all_proxy` environment variable for SOCKS support.

```bash
all_proxy=socks5://localhost:8080 wget http://localhost:8081/test.txt -O -
```

In all cases, the expected output is the content of the `test.txt` file:
`Bonjour, ceci est un test de téléchargement.`

## Project Files

*   `main.go`: The source code for the main proxy server application.
*   `test_server/main.go`: The source code for the simple HTTP test server.
*   `test_server/test.txt`: A simple text file used for testing downloads through the proxy.
*   `plan.md`: The development and testing plan followed to create this project.
*   `README.md`: This file.
