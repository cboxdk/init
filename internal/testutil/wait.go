// Package testutil provides common testing utilities for cbox-init.
package testutil

import (
	"fmt"
	"testing"
	"time"
)

// DefaultTimeout is the default timeout for polling operations.
const DefaultTimeout = 5 * time.Second

// DefaultInterval is the default polling interval.
const DefaultInterval = 10 * time.Millisecond

// WaitForCondition polls until condition returns true or timeout is reached.
// Returns an error if the condition is not met within the timeout.
func WaitForCondition(t *testing.T, timeout time.Duration, condition func() bool, description string) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return nil
		}
		time.Sleep(DefaultInterval)
	}
	return fmt.Errorf("timeout waiting for %s after %v", description, timeout)
}

// MustWaitForCondition is like WaitForCondition but fails the test on timeout.
func MustWaitForCondition(t *testing.T, timeout time.Duration, condition func() bool, description string) {
	t.Helper()
	if err := WaitForCondition(t, timeout, condition, description); err != nil {
		t.Fatalf("%v", err)
	}
}

// WaitForConditionWithInterval is like WaitForCondition but allows custom interval.
func WaitForConditionWithInterval(t *testing.T, timeout, interval time.Duration, condition func() bool, description string) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("timeout waiting for %s after %v", description, timeout)
}

// Eventually asserts that condition becomes true within timeout.
// This is the most commonly used function for replacing time.Sleep patterns.
func Eventually(t *testing.T, condition func() bool, description string, timeoutOpts ...time.Duration) {
	t.Helper()
	timeout := DefaultTimeout
	if len(timeoutOpts) > 0 {
		timeout = timeoutOpts[0]
	}
	MustWaitForCondition(t, timeout, condition, description)
}

// WaitForProcessStart polls until checkFn returns true, indicating process has started.
// This replaces the common pattern: time.Sleep(100 * time.Millisecond) // wait for process
func WaitForProcessStart(t *testing.T, checkFn func() bool) {
	t.Helper()
	MustWaitForCondition(t, 2*time.Second, checkFn, "process to start")
}

// WaitForProcessStop polls until checkFn returns true, indicating process has stopped.
func WaitForProcessStop(t *testing.T, checkFn func() bool) {
	t.Helper()
	MustWaitForCondition(t, 5*time.Second, checkFn, "process to stop")
}

// WaitForScale polls until scale reaches expected value.
func WaitForScale(t *testing.T, getScale func() int, expected int) {
	t.Helper()
	MustWaitForCondition(t, 5*time.Second, func() bool {
		return getScale() == expected
	}, fmt.Sprintf("scale to reach %d", expected))
}

// WaitForScaleAtLeast polls until scale reaches at least the expected value.
func WaitForScaleAtLeast(t *testing.T, getScale func() int, expected int) {
	t.Helper()
	MustWaitForCondition(t, 5*time.Second, func() bool {
		return getScale() >= expected
	}, fmt.Sprintf("scale to reach at least %d", expected))
}

// WaitForScaleAtMost polls until scale reaches at most the expected value.
func WaitForScaleAtMost(t *testing.T, getScale func() int, expected int) {
	t.Helper()
	MustWaitForCondition(t, 5*time.Second, func() bool {
		return getScale() <= expected
	}, fmt.Sprintf("scale to reach at most %d", expected))
}

// WaitForState polls until state matches expected value.
func WaitForState(t *testing.T, getState func() string, expected string) {
	t.Helper()
	MustWaitForCondition(t, 5*time.Second, func() bool {
		return getState() == expected
	}, fmt.Sprintf("state to become %q", expected))
}

// WaitForPIDChange polls until PID changes from the original value.
func WaitForPIDChange(t *testing.T, getPID func() int, originalPID int) {
	t.Helper()
	MustWaitForCondition(t, 5*time.Second, func() bool {
		pid := getPID()
		return pid > 0 && pid != originalPID
	}, "PID to change")
}
