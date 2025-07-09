package main

import (
	"log"
	"net"
	"net/http"
	"os"
)

const (
	port = "8081"
)

// isPortAvailable checks if a TCP port is available to listen on.
func isPortAvailable(p string) bool {
	ln, err := net.Listen("tcp", ":"+p)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func main() {
	// Check if the port is available before starting the server.
	if !isPortAvailable(port) {
		log.Fatalf("Port %s is already in use. Please free it and try again.", port)
	}

	// Use the current directory for the file server.
	// Note: This requires the program to be run from the 'test_server' directory.
	fs := http.FileServer(http.Dir("."))
	http.Handle("/", fs)

	log.Printf("Test server starting on port %s...", port)
	log.Printf("Serving files from the '%s' directory.", func() string {
		dir, _ := os.Getwd()
		return dir
	}())

	// Start the server.
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}