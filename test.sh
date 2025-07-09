#!/bin/bash

# ==============================================================================
#
#  Fully Automated Proxy Server Test Suite
#
#  This script handles the entire test process:
#  1. Frees required ports by stopping any running services.
#  2. Compiles the applications if they don't exist.
#  3. Starts both the proxy and test servers, redirecting their output to logs.
#  4. Runs all tests.
#  5. Stops all servers and cleans up.
#
# ==============================================================================

# --- Configuration ---
set -e
set -o pipefail

# Ports and App locations
PROXY_PORT="8080"
TEST_SERVER_PORT="8081"
PROXY_APP_NAME="proxy_app"
TEST_SERVER_DIR="test_server"
TEST_SERVER_APP_NAME="test_server_app"

# PID file locations
PROXY_PID_FILE="proxy_server.pid"
TEST_SERVER_PID_FILE="test_server.pid"

# Test configuration
PROXY_URL="http://localhost:$PROXY_PORT"
SOCKS5_URL="socks5://localhost:$PROXY_PORT"
TEST_URL="http://localhost:$TEST_SERVER_PORT/test.txt"
EXPECTED_OUTPUT="Bonjour, ceci est un test de téléchargement."

FAILED_TESTS=0
TOTAL_TESTS=0

# --- Helper Functions ---
print_header() {
    echo ""
    echo "====================================================="
    echo "  $1"
    echo "====================================================="
}

cleanup() {
    print_header "Cleaning Up"
    # Stop servers based on PID files
    for pid_file in "$PROXY_PID_FILE" "$TEST_SERVER_PID_FILE"; do
        if [ -f "$pid_file" ]; then
            local pid
            pid=$(cat "$pid_file")
            if [ -n "$pid" ] && ps -p "$pid" > /dev/null; then
                echo "Stopping process with PID $pid from $pid_file..."
                kill -9 "$pid" 2>/dev/null || true
            fi
            rm -f "$pid_file"
        fi
    done
    echo "Cleanup complete. Server logs are in 'Proxy_Server.log' and 'Test_Server.log'."
}

# Trap the script's exit to run the cleanup function automatically
trap cleanup EXIT

free_port() {
    local port=$1
    echo "Checking if port $port is in use..."
    local pids
    pids=$(lsof -t -i:"$port" 2>/dev/null || true)
    
    if [ -n "$pids" ]; then
        echo "Port $port is in use by the following PIDs: $pids"
        for pid in $pids; do
            echo "Terminating process $pid..."
            kill -9 "$pid"
        done
        sleep 0.5 # Give the OS a moment to release the port
        echo "Processes terminated."
    else
        echo "Port $port is free."
    fi
}

compile_if_needed() {
    local app_path=$1
    local source_path=$2
    local app_name=$3
    
    if [ ! -f "$app_path" ]; then
        print_header "Compiling $app_name"
        echo "$app_name not found. Building it now..."
        go build -o "$app_path" "$source_path"
        echo "Build complete."
    fi
}

start_server() {
    local server_name=$1
    local app_path=$2
    local directory=$3
    local pid_file=$4
    local log_file
    log_file="$(pwd)/${server_name// /_}.log"

    print_header "Starting $server_name"
    echo "Redirecting server output to '$log_file'"
    
    pushd "$directory" > /dev/null
    # Start the server in the background and save its PID
    ./"$(basename "$app_path")" --debug > "$log_file" 2>&1 &
    local pid=$!
    echo "$pid" > "../$pid_file"
    popd > /dev/null
    
    echo "$server_name started with PID: $pid."
}

wait_for_server() {
    local port=$1
    local server_name=$2
    echo "Waiting for $server_name to initialize on port $port..."
    for i in {1..10}; do # Wait up to 5 seconds
        if nc -z localhost "$port" 2>/dev/null; then
            echo "$server_name is running."
            return
        fi
        sleep 0.5
    done
    echo "Error: Failed to start $server_name in time. Check its log file for errors."
    exit 1
}

run_test() {
    local test_name="$1"
    shift
    local command=("$@")
    TOTAL_TESTS=$((TOTAL_TESTS + 1))

    print_header "Test: $test_name"
    echo "▶️  Running command: ${command[*]}"
    echo ""

    set +e
    local stdout
    local stderr
    local exit_code
    local err_file
    err_file=$(mktemp)
    
    stdout=$("${command[@]}" 2> "$err_file")
    exit_code=$?
    stderr=$(<"$err_file")
    rm "$err_file"
    set -e

    if [ "$exit_code" -eq 0 ] && [ "$stdout" == "$EXPECTED_OUTPUT" ]; then
        echo "✅ PASS"
    else
        echo "❌ FAIL"
        echo "   Exit Code: $exit_code"
        echo "   Expected Output: '$EXPECTED_OUTPUT'"
        echo "   Actual Output:   '$stdout'"
        if [ -n "$stderr" ]; then
            echo "   Error Output (stderr):"
            echo "$stderr" | sed 's/^/     /'
        fi
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
}

# --- Main Execution ---
print_header "Setup & Prerequisite Checks"
free_port $PROXY_PORT
free_port $TEST_SERVER_PORT

compile_if_needed "$PROXY_APP_NAME" "main.go" "Proxy Server"
compile_if_needed "$TEST_SERVER_DIR/$TEST_SERVER_APP_NAME" "$TEST_SERVER_DIR/main.go" "Test Server"

start_server "Proxy Server" "$PROXY_APP_NAME" "." "$PROXY_PID_FILE"
start_server "Test Server" "$TEST_SERVER_DIR/$TEST_SERVER_APP_NAME" "$TEST_SERVER_DIR" "$TEST_SERVER_PID_FILE"

wait_for_server $PROXY_PORT "Proxy Server"
wait_for_server $TEST_SERVER_PORT "Test Server"

# --- Run Tests ---
run_test "curl (HTTP)" \
    curl --silent --show-error --max-time 3 --proxy "$PROXY_URL" "$TEST_URL"

run_test "curl (SOCKS5)" \
    curl --silent --show-error --max-time 3 --proxy "$SOCKS5_URL" "$TEST_URL"

run_test "wget (HTTP)" \
    wget --timeout=3 -e "http_proxy=$PROXY_URL" "$TEST_URL" -qO -

run_test "wget (SOCKS5)" \
    bash -c "all_proxy=$SOCKS5_URL wget --timeout=3 $TEST_URL -qO -"

# --- Summary ---
print_header "Test Summary"
if [ "$FAILED_TESTS" -eq 0 ]; then
    echo "🎉 All $TOTAL_TESTS tests passed successfully! 🎉"
    exit 0
else
    echo "🔥 $FAILED_TESTS out of $TOTAL_TESTS tests failed. 🔥"
    exit 1
fi
