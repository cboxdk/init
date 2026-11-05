package snapshot

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/cboxdk/phpfpm"
)

// CboxFPMQuiescence configures the single main pool in a Cbox PHP image.
// Read status on its direct loopback listener only while proxy admission is closed.
func CboxFPMQuiescence(pool, upstream string) (func(context.Context) error, error) {
	address, err := netip.ParseAddrPort(upstream)
	if err != nil || !address.Addr().IsLoopback() || address.Port() == 0 {
		return nil, errors.New("FPM quiescence requires a loopback upstream IP and port")
	}
	if pool != "www" {
		return nil, errors.New("single-pool FPM quiescence requires the www pool")
	}
	targets := []phpfpm.Target{{Name: pool, Socket: "tcp://" + upstream, StatusPath: "/fpm-status", Timeout: time.Second}}
	return func(ctx context.Context) error { return CheckFPMQuiescence(ctx, targets) }, nil
}

// CheckFPMQuiescence checks the complete operator-supplied set of FPM pools
// through their native, main-pool status listeners. All application admissions
// must remain closed around this call and checkpoint. This cannot discover
// omitted pools, detached background work or direct connections bypassing the
// proxy, so it is not a standalone checkpoint authorization mechanism.
func CheckFPMQuiescence(ctx context.Context, targets []phpfpm.Target) error {
	if len(targets) == 0 {
		return errors.New("no FPM pools configured for quiescence")
	}
	for _, target := range targets {
		if target.Name == "" || !strings.HasPrefix(target.StatusPath, "/") || strings.ContainsAny(target.StatusPath, "?#") {
			return errors.New("FPM quiescence requires an exact pool name and native status path")
		}
		pool, err := phpfpm.ReadStatus(ctx, target)
		if err != nil {
			return fmt.Errorf("FPM status unavailable: %w", err)
		}
		if err := validateIdleFPMPool(pool, target); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func validateIdleFPMPool(pool phpfpm.Pool, target phpfpm.Target) error {
	// Do not use Scrape's adjusted active count: the native status request must
	// be the only active worker, with every worker accounted for explicitly.
	if pool.Name != target.Name || pool.TotalProcesses < 1 || int64(len(pool.Processes)) != pool.TotalProcesses ||
		pool.ListenQueue != 0 || pool.ActiveProcesses != 1 || pool.IdleProcesses != pool.TotalProcesses-1 {
		return errors.New("FPM pool is busy or status is incomplete")
	}
	seen := make(map[int]bool, len(pool.Processes))
	idle, status := int64(0), 0
	for _, process := range pool.Processes {
		if process.PID <= 0 || seen[process.PID] {
			return errors.New("FPM worker identity is missing or duplicated")
		}
		seen[process.PID] = true
		switch process.State {
		case "Idle":
			idle++
		case "Running":
			if process.RequestMethod != "GET" || process.RequestURI != target.StatusPath+"?json&full" {
				return errors.New("FPM worker is executing application work")
			}
			status++
		default:
			return errors.New("FPM worker is not idle")
		}
	}
	if status != 1 || idle != pool.IdleProcesses {
		return errors.New("FPM worker states do not match pool counters")
	}
	return nil
}
