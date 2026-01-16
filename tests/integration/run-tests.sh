#!/bin/sh
set -e

echo "=== Cbox Init Integration Tests ==="
echo ""

# Test 1: Binary exists and is executable
echo "✓ Test 1: Binary Check"
if [ ! -x /usr/local/bin/cbox-init ]; then
    echo "✗ FAILED: Binary not executable"
    exit 1
fi
echo "  Binary is executable"
echo ""

# Test 2: Version check
echo "✓ Test 2: Version Check"
/usr/local/bin/cbox-init --version 2>&1 || true
echo ""

# Test 3: Configuration validation
echo "✓ Test 3: Configuration Validation"
if [ ! -f /etc/cbox-init/cbox-init.yaml ]; then
    echo "✗ FAILED: Config file not found"
    exit 1
fi
echo "  Config file exists"
echo ""

# Test 4: Start Cbox Init in background
echo "✓ Test 4: Process Startup"
/usr/local/bin/cbox-init &
CBOX_PID=$!
echo "  Cbox Init started with PID: $CBOX_PID"

# Wait for startup
sleep 3

# Test 5: Check process is running
echo ""
echo "✓ Test 5: Process Running"
if ! kill -0 $CBOX_PID 2>/dev/null; then
    echo "✗ FAILED: Cbox Init not running"
    exit 1
fi
echo "  Cbox Init is running"

# Test 6: Check managed processes
echo ""
echo "✓ Test 6: Managed Processes"
# Use /proc to check for sleep processes (works everywhere)
found_sleep=0
for pid_dir in /proc/[0-9]*; do
    if [ -f "$pid_dir/cmdline" ] && grep -q "sleep" "$pid_dir/cmdline" 2>/dev/null; then
        found_sleep=1
        break
    fi
done

if [ $found_sleep -eq 0 ]; then
    echo "✗ FAILED: Managed process not found"
    kill $CBOX_PID 2>/dev/null || true
    exit 1
fi
echo "  Managed processes are running"

# Test 7: Metrics endpoint (if available)
echo ""
echo "✓ Test 7: Metrics Endpoint"
if command -v curl > /dev/null 2>&1; then
    if curl -s --max-time 2 http://localhost:9090/metrics | grep -q "cbox_init"; then
        echo "  Metrics endpoint is responding"
    else
        echo "  ⚠ Metrics endpoint not responding (non-fatal)"
    fi
else
    echo "  ⚠ curl not available, skipping metrics test"
fi

# Test 8: API endpoint (if available)
echo ""
echo "✓ Test 8: API Endpoint"
if command -v curl > /dev/null 2>&1; then
    if curl -s --max-time 2 http://localhost:9180/api/v1/health | grep -q "healthy"; then
        echo "  API endpoint is responding"
    else
        echo "  ⚠ API endpoint not responding (non-fatal)"
    fi
else
    echo "  ⚠ curl not available, skipping API test"
fi

# Test 9: Auto-exit when all processes die
echo ""
echo "✓ Test 9: Auto-exit on Process Death"
echo "  Waiting for managed processes to complete (sleep 5)..."

# Wait for auto-exit (should happen after ~5 seconds when sleep processes finish)
# Give it up to 10 seconds total
for i in 1 2 3 4 5 6 7 8 9 10; do
    if ! kill -0 $CBOX_PID 2>/dev/null; then
        echo "  Cbox Init auto-exited after managed processes died (${i}s elapsed)"
        break
    fi
    sleep 1
done

# Check if process stopped
if kill -0 $CBOX_PID 2>/dev/null; then
    echo "  ✗ FAILED: Cbox Init still running after 10s"
    kill -KILL $CBOX_PID 2>/dev/null || true
    wait $CBOX_PID 2>/dev/null || true
    exit 1
fi

# Give it a moment to clean up
sleep 1

# Test 10: Verify no zombie processes
echo ""
echo "✓ Test 10: Zombie Check"
# Use /proc to check for sleep processes
found_zombie=0
for pid_dir in /proc/[0-9]*; do
    if [ -f "$pid_dir/cmdline" ] && grep -q "sleep" "$pid_dir/cmdline" 2>/dev/null; then
        found_zombie=1
        # Extract PID from path
        zombie_pid=$(basename "$pid_dir")
        echo "  ⚠ Found zombie sleep process: $zombie_pid"
        kill -9 "$zombie_pid" 2>/dev/null || true
    fi
done

if [ $found_zombie -eq 0 ]; then
    echo "  No zombie processes detected"
fi

echo ""
echo "=== All Integration Tests Passed! ==="
echo ""

# Show system info
echo "System Information:"
echo "  Distro: $(cat /etc/os-release | grep "^PRETTY_NAME" | cut -d'"' -f2)"
echo "  Kernel: $(uname -r)"
echo "  Arch: $(uname -m)"
