package signals

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/testutil"
)

func TestIsPID1(t *testing.T) {
	// In normal test execution, we're not PID 1
	result := IsPID1()
	if result {
		t.Error("IsPID1() returned true, but we're not running as PID 1")
	}

	// Verify we're getting a sensible PID
	pid := os.Getpid()
	if pid <= 0 {
		t.Errorf("os.Getpid() returned invalid PID: %d", pid)
	}
}

func TestReapAll_NoZombies(t *testing.T) {
	// When there are no zombies to reap, reapAll should return without error
	// This tests the error/no-children path
	reapAll()
	// If we get here without panic, the test passes
}

func TestReapAll_WithZombie(t *testing.T) {
	// Skip if not on Unix-like system
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping zombie reaping test in CI environment - use mock tests instead")
	}

	// Create a child process that exits immediately
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start child process: %v", err)
	}

	// Poll until process exits (becomes zombie) - more reliable than fixed sleep
	testutil.Eventually(t, func() bool {
		// Signal 0 doesn't send a signal but checks if process exists
		// When the process exits but isn't reaped, it's a zombie
		// We can't directly detect zombie state, so we wait briefly and try to reap
		reapAll()
		// Check if process is gone
		err := cmd.Process.Signal(syscall.Signal(0))
		return err != nil // Process is gone
	}, "zombie process to be reaped", 2*time.Second)

	// Cleanup: try waiting manually if somehow still around
	_ = cmd.Wait()
}

func TestReapZombies_Goroutine(t *testing.T) {
	// Test that ReapZombies runs as a goroutine without blocking
	done := make(chan bool)
	started := make(chan bool)

	go func() {
		close(started)
		ReapZombies(1 * time.Second) // Pass default interval
	}()

	// Wait for goroutine to start
	<-started

	// Give it a moment to run the ticker
	time.Sleep(50 * time.Millisecond)

	// If we can send to done, goroutine is still running (which is expected)
	select {
	case done <- true:
		// Goroutine received, unexpected since ReapZombies runs forever
	default:
		// Expected: ReapZombies is running its ticker loop
	}

	// The goroutine will be cleaned up when the test ends
}

func TestReapZombies_CustomInterval(t *testing.T) {
	// Test that ReapZombies accepts custom interval
	started := make(chan bool)

	go func() {
		close(started)
		ReapZombies(500 * time.Millisecond) // Custom interval
	}()

	// Wait for goroutine to start
	<-started

	// Give it a moment to run the ticker
	time.Sleep(50 * time.Millisecond)

	// Test passes if we get here without panic or deadlock
}

func TestReapZombies_ZeroIntervalFallback(t *testing.T) {
	// Test that ReapZombies handles zero interval (should default to 1s)
	started := make(chan bool)

	go func() {
		close(started)
		ReapZombies(0) // Zero interval should default to 1 second
	}()

	// Wait for goroutine to start
	<-started

	// Give it a moment to run the ticker
	time.Sleep(50 * time.Millisecond)

	// Test passes if we get here without panic or deadlock
}

func TestReapZombies_NegativeIntervalFallback(t *testing.T) {
	// Test that ReapZombies handles negative interval (should default to 1s)
	started := make(chan bool)

	go func() {
		close(started)
		ReapZombies(-1 * time.Second) // Negative interval should default to 1 second
	}()

	// Wait for goroutine to start
	<-started

	// Give it a moment to run the ticker
	time.Sleep(50 * time.Millisecond)

	// Test passes if we get here without panic or deadlock
}

func TestIsPID1_ReturnType(t *testing.T) {
	// Verify IsPID1 returns a boolean
	result := IsPID1()
	if result && os.Getpid() != 1 {
		t.Error("IsPID1 returned true but os.Getpid() != 1")
	}
	if !result && os.Getpid() == 1 {
		t.Error("IsPID1 returned false but os.Getpid() == 1")
	}
}

// Mock-based tests for CI environment
// These tests use the mockable waitFunc to test zombie reaping logic

func TestReaper_CapturesSupervisedStatus(t *testing.T) {
	originalWait := getWaitFunc()
	defer func() { setWaitFunc(originalWait) }()

	const supervisedPID = 4242
	const orphanPID = 9999

	// Clean state.
	UnregisterSupervised(supervisedPID)
	UnregisterSupervised(orphanPID)

	// The supervisor registers its child before waiting on it.
	RegisterSupervised(supervisedPID)

	// Reaper wins the race and reaps both the supervised child (exit 7) and an
	// unrelated orphan.
	var status syscall.WaitStatus = 7 << 8 // WaitStatus encodes exit code in bits 8-15
	call := 0
	setWaitFunc(func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (int, error) {
		call++
		switch call {
		case 1:
			*wstatus = status
			return supervisedPID, nil
		case 2:
			*wstatus = 0
			return orphanPID, nil
		default:
			return -1, syscall.ECHILD
		}
	})

	reapAll()

	// The supervised child's status must be recoverable...
	ws, ok := TakeReapedStatus(supervisedPID)
	if !ok {
		t.Fatalf("expected captured status for supervised pid %d", supervisedPID)
	}
	if !ws.Exited() || ws.ExitStatus() != 7 {
		t.Errorf("recovered wrong status: exited=%v code=%d", ws.Exited(), ws.ExitStatus())
	}

	// ...and taking it clears the entry (idempotent, no leak).
	if _, ok := TakeReapedStatus(supervisedPID); ok {
		t.Error("status should be cleared after first take")
	}

	// The orphan was reaped but never stashed.
	if _, ok := TakeReapedStatus(orphanPID); ok {
		t.Error("orphan pid should not have a captured status")
	}
}

func TestReaper_UnregisterDropsStatus(t *testing.T) {
	originalWait := getWaitFunc()
	defer func() { setWaitFunc(originalWait) }()

	const pid = 5555
	RegisterSupervised(pid)
	call := 0
	setWaitFunc(func(p int, ws *syscall.WaitStatus, o int, r *syscall.Rusage) (int, error) {
		call++
		if call == 1 {
			*ws = 0
			return pid, nil // reaper captures the supervised child's status
		}
		return -1, syscall.ECHILD
	})
	reapAll()

	// Supervisor won its own Wait() (ProcessState set) and unregisters.
	UnregisterSupervised(pid)
	if _, ok := TakeReapedStatus(pid); ok {
		t.Error("UnregisterSupervised should drop any captured status")
	}
}

func TestReapAll_MockedSingleZombie(t *testing.T) {
	// Save original and restore after test
	originalWait := getWaitFunc()
	defer func() { setWaitFunc(originalWait) }()

	// Track how many times wait was called
	callCount := 0
	mockWait := func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (int, error) {
		callCount++
		if callCount == 1 {
			// First call: return a reaped zombie
			return 12345, nil
		}
		// Second call: no more children
		return -1, syscall.ECHILD
	}
	setWaitFunc(mockWait)

	// Run reapAll
	reapAll()

	// Should have called wait twice (once for zombie, once for no more)
	if callCount != 2 {
		t.Errorf("Expected wait to be called 2 times, got %d", callCount)
	}
}

func TestReapAll_MockedMultipleZombies(t *testing.T) {
	originalWait := getWaitFunc()
	defer func() { setWaitFunc(originalWait) }()

	callCount := 0
	mockWait := func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (int, error) {
		callCount++
		switch callCount {
		case 1:
			return 100, nil // First zombie
		case 2:
			return 200, nil // Second zombie
		case 3:
			return 300, nil // Third zombie
		default:
			return -1, syscall.ECHILD // No more
		}
	}
	setWaitFunc(mockWait)

	reapAll()

	// Should have called wait 4 times (3 zombies + 1 no more)
	if callCount != 4 {
		t.Errorf("Expected wait to be called 4 times, got %d", callCount)
	}
}

func TestReapAll_MockedNoZombies(t *testing.T) {
	originalWait := getWaitFunc()
	defer func() { setWaitFunc(originalWait) }()

	callCount := 0
	mockWait := func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (int, error) {
		callCount++
		return -1, syscall.ECHILD // No children
	}
	setWaitFunc(mockWait)

	reapAll()

	// Should have called wait exactly once
	if callCount != 1 {
		t.Errorf("Expected wait to be called 1 time, got %d", callCount)
	}
}

func TestReapAll_MockedErrorHandling(t *testing.T) {
	originalWait := getWaitFunc()
	defer func() { setWaitFunc(originalWait) }()

	callCount := 0
	mockWait := func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (int, error) {
		callCount++
		return 0, errors.New("unexpected error")
	}
	setWaitFunc(mockWait)

	// Should not panic on error
	reapAll()

	if callCount != 1 {
		t.Errorf("Expected wait to be called 1 time, got %d", callCount)
	}
}

func TestReapCount_MockedZombies(t *testing.T) {
	originalWait := getWaitFunc()
	defer func() { setWaitFunc(originalWait) }()

	// Create a mock that returns specific number of zombies
	zombieCount := 5
	callNum := 0
	var mu sync.Mutex
	mockWait := func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		callNum++
		if callNum <= zombieCount {
			return 1000 + callNum, nil
		}
		return -1, syscall.ECHILD
	}
	setWaitFunc(mockWait)

	count := ReapCount()

	if count != zombieCount {
		t.Errorf("Expected ReapCount to return %d, got %d", zombieCount, count)
	}
}

func TestReapCount_MockedNoZombies(t *testing.T) {
	originalWait := getWaitFunc()
	defer func() { setWaitFunc(originalWait) }()

	mockWait := func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (int, error) {
		return -1, syscall.ECHILD
	}
	setWaitFunc(mockWait)

	count := ReapCount()

	if count != 0 {
		t.Errorf("Expected ReapCount to return 0, got %d", count)
	}
}

func TestWaitFuncParameters(t *testing.T) {
	originalWait := getWaitFunc()
	defer func() { setWaitFunc(originalWait) }()

	var capturedPid int
	var capturedOptions int

	mockWait := func(pid int, wstatus *syscall.WaitStatus, options int, rusage *syscall.Rusage) (int, error) {
		capturedPid = pid
		capturedOptions = options
		return -1, syscall.ECHILD
	}
	setWaitFunc(mockWait)

	reapAll()

	// Should call with -1 (any child) and WNOHANG (non-blocking)
	if capturedPid != -1 {
		t.Errorf("Expected pid -1, got %d", capturedPid)
	}
	if capturedOptions != syscall.WNOHANG {
		t.Errorf("Expected options WNOHANG (%d), got %d", syscall.WNOHANG, capturedOptions)
	}
}
