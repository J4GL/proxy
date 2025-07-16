# Proxy Server Reproduction Plan

This document provides instructions to create a dual-protocol (HTTP and SOCKS5) proxy server in Go. Follow the steps below to create the necessary files and implement the logic.

## 1. File and Directory Setup

First, create the project structure. Open your terminal and run these commands:

```sh
mkdir -p test_server
touch go.mod config.yaml main.go test_server/main.go
```

## 2. File Implementation

Now, you will implement the logic for each file.

### `go.mod`

1.  Declare a Go module named `proxy`.
2.  Specify Go version `1.24.2`.
3.  Add a dependency on `gopkg.in/yaml.v2` version `v2.4.0`.

### `config.yaml`

1.  Create a YAML file.
2.  Add a top-level key named `allowed_ips`.
3.  The value for `allowed_ips` should be a list of strings, where each string is an IP address.
4.  For local testing, add `"127.0.0.1"` and `"::1"` to the list.

### `main.go` (Proxy Server)

This is the core of the proxy server.

1.  **Package and Imports**:
    *   Use package `main`.
    *   Import necessary packages for networking (`net`, `net/http`), I/O (`io`, `io/ioutil`, `bufio`), logging (`log`), command-line flags (`flag`), string/number conversion (`strings`, `strconv`), binary data handling (`encoding/binary`), and YAML parsing (`gopkg.in/yaml.v2`).

2.  **Constants and Structs**:
    *   Define constants for the proxy port (`"8080"`) and SOCKS5 protocol values (e.g., `socks5Version = 0x05`).
    *   Create a `Config` struct with an `AllowedIPs` field (a slice of strings) to match the structure of `config.yaml`.

3.  **Configuration Loading**:
    *   Implement a `loadConfig(path string)` function that reads the `config.yaml` file, unmarshals it into the `Config` struct, and returns a `map[string]bool` for efficient IP address lookups.

4.  **Port Availability**:
    *   Implement an `isPortAvailable(port string)` function that checks if a given TCP port is free to listen on.

5.  **Main Function**:
    *   Add a boolean command-line flag (`-d` or `-debug`) to enable debug logging.
    *   Check if the proxy port is available; if not, log a fatal error.
    *   Call `loadConfig` to load the allowed IPs.
    *   Create a TCP listener on the proxy port.
    *   Enter an infinite loop to accept incoming connections (`listener.Accept()`).
    *   For each new connection, spawn a new goroutine that calls a `handleConnection` function.

6.  **Connection Handling**:
    *   Implement `handleConnection(conn net.Conn, ...)`:
        *   Verify the client's remote IP address is present in the allowed IPs map. If not, close the connection.
        *   Use `bufio.Reader.Peek(1)` to look at the first byte of the incoming data without consuming it.
        *   If the first byte matches the SOCKS5 version constant, pass the connection to a `handleSocks5` function.
        *   Otherwise, pass it to an `handleHTTP` function.

7.  **HTTP Handler**:
    *   Implement `handleHTTP(...)`:
        *   Read the full HTTP request from the client connection.
        *   Establish a new TCP connection to the destination server specified in the request's `Host` header.
        *   If the request method is `CONNECT` (for HTTPS), respond to the client with `HTTP/1.1 200 Connection established`.
        *   If it's any other method, write the original request to the destination server.
        *   Use `io.Copy` in separate goroutines to relay data in both directions between the client and the destination server.

8.  **SOCKS5 Handler**:
    *   Implement `handleSocks5(...)`:
        *   Perform the SOCKS5 handshake: read the version and method count, then select the "No Authentication" method (`0x00`) and send it back to the client.
        *   Read the client's request details (command, address type, destination address, and port).
        *   Handle different address types (IPv4, domain name, IPv6) to correctly parse the destination address.
        *   Establish a TCP connection to the parsed destination address and port.
        *   Send a SOCKS5 success reply back to the client.
        *   Use `io.Copy` to relay data between the client and the destination.

### `test_server/main.go` (Test Server)

This is a simple server to help test the proxy.

1.  **Package and Imports**:
    *   Use package `main`.
    *   Import packages for logging (`log`), networking (`net`, `net/http`), and the operating system (`os`).

2.  **Main Function**:
    *   Define a constant for the server's port (e.g., `"8081"`).
    *   Optionally, use the `isPortAvailable` function from the proxy to check if the port is free.
    *   Create a simple file server using `http.FileServer(http.Dir("."))`.
    *   Start the HTTP server to listen on the specified port.

## 3. How to Run and Test

1.  **Run the Test Server**:
    ```sh
    go run test_server/main.go
    ```

2.  **Run the Proxy Server**:
    In a separate terminal:
    ```sh
    go run main.go
    ```

3.  **Test the Proxy**:
    In a third terminal, use `curl` to make requests through the proxy.

    *   **HTTP Test**:
        ```sh
        curl -x http://127.0.0.1:8080 http://127.0.0.1:8081/
        ```
    *   **SOCKS5 Test**:
        ```sh
        curl --socks5 127.0.0.1:8080 http://127.0.0.1:8081/
        ```

Both `curl` commands should successfully connect to the test server through your proxy and return a listing of the files in the `test_server` directory.
