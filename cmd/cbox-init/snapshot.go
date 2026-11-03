package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/cboxdk/init/internal/process"
	"github.com/cboxdk/init/internal/snapshot"
)

// The warm tier, wired up.
//
// Everything here is opt-in and every way of not being eligible ends the same
// way: cbox-init logs why and runs the container exactly as it always did. A
// node without the agent, a container with more than one supervised process, a
// missing upstream address — none of them is an error, because a container that
// cannot sleep is a container that works.

// upstreamEnv names the address the workload itself listens on inside the
// container, which is not the port clients connect to: cbox-init holds that one
// so the handshake completes while the workload is asleep. There is no default,
// on purpose — guessing a port would produce a proxy that listens in front of
// nothing and a container that fails every request instead of serving them.
const upstreamEnv = "CBOX_SNAPSHOT_UPSTREAM"

// idleEnv overrides the idle timeout the agent hands down at registration.
const idleEnv = "CBOX_SNAPSHOT_IDLE"

// listenEnv keeps the service proxy available even when registration is refused.
const listenEnv = "CBOX_SNAPSHOT_LISTEN"

// startWarmTier registers with the node agent, puts the proxy in front of the
// workload and starts the idle loop. It returns nil when the container is not
// eligible, having said why.
func startWarmTier(ctx context.Context, pm *process.Manager, log *slog.Logger) (coordinator *snapshot.Coordinator) {
	upstream := os.Getenv(upstreamEnv)
	if upstream == "" {
		log.Info("warm tier not enabled for this workload",
			"reason", upstreamEnv+" is not set, so there is no address to proxy to")
		return nil
	}

	proxy := snapshot.NewProxy(snapshot.ProxyOptions{
		Upstream:    upstream,
		Log:         log,
		DialTimeout: 15 * time.Second,
	})
	var ln net.Listener
	if address := os.Getenv(listenEnv); address != "" {
		var err error
		ln, err = net.Listen("tcp", address)
		if err != nil {
			log.Error("could not hold the configured service port", "error", err)
			return nil
		}
	}
	defer func() {
		if ln == nil {
			return
		}
		if coordinator != nil {
			proxy.SetWaker(coordinator)
		}
		go func() {
			if err := proxy.Serve(ctx, ln); err != nil {
				log.Error("the service port proxy stopped", "error", err)
			}
		}()
	}()

	socket := snapshot.SocketPath("")
	if _, err := os.Stat(socket); err != nil {
		log.Info("warm tier not offered on this node", "socket", socket)
		return nil
	}

	if os.Getpid() != 1 {
		log.Warn("warm tier requires namespace PID 1 for safe failure fencing")
		return nil
	}

	pid, stream, stderr, err := pm.SnapshotTarget()
	if err != nil {
		log.Warn("warm tier unavailable for this container; it will run without sleeping", "error", err)
		return nil
	}

	client, err := snapshot.Dial(socket)
	if err != nil {
		if errors.Is(err, snapshot.ErrUnavailable) {
			log.Info("no snapshot agent on this node; running without the warm tier", "socket", socket)
			return nil
		}
		log.Warn("could not reach the snapshot agent; running without the warm tier", "error", err)
		return nil
	}

	registerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	policy, err := client.Register(registerCtx, pid, stream, stderr)
	cancel()
	if err != nil {
		// A refusal here is the design working: tty: true and user-namespaced
		// containers are rejected at registration rather than at checkpoint
		// time, so the first failure is now and visible instead of minutes
		// later on a workload the operator was told would sleep.
		log.Warn("the node agent refused this container; it will run without sleeping", "error", err)
		_ = client.Close()
		return nil
	}

	idle := policy.IdleTimeout
	if override := os.Getenv(idleEnv); override != "" {
		if d, err := time.ParseDuration(override); err == nil {
			idle = d
		} else {
			log.Warn("ignoring an unparseable idle override", idleEnv, override, "error", err)
		}
	}

	if ln != nil && ln.Addr().(*net.TCPAddr).Port != policy.Port {
		log.Error("snapshot policy does not match the configured service port", "port", policy.Port)
		_ = client.Close()
		return nil
	}
	if ln == nil {
		ln, err = net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(policy.Port)))
		if err != nil {
			log.Error("could not hold the service port; running without the warm tier", "error", err)
			_ = client.Close()
			return nil
		}
	}

	coordinator = snapshot.NewCoordinator(snapshot.CoordinatorOptions{
		Fence: func(err error) {
			// Exiting namespace PID 1 makes Linux terminate every member,
			// including a late/untracked CRIU restore. Kubelet then cold starts
			// on the existing volume; an in-place restart cannot provide fencing.
			log.Error("terminating PID 1 after uncertain snapshot operation", "error", err)
			os.Exit(70)
		},
		Control:  client,
		Workload: pm,
		Proxy:    proxy,
		Log:      log,
		Idle:     idle,
	})
	go coordinator.Run(ctx)

	log.Info("warm tier active",
		"port", policy.Port, "upstream", upstream, "idle", idle, "child_pid", pid)
	return coordinator
}

// warmTierStatus is what an operator needs to be able to ask, because the
// failure this design is most likely to ship is a container that quietly
// stopped sleeping.
func warmTierStatus(c *snapshot.Coordinator) string {
	if c == nil {
		return "off"
	}
	if reason := c.Disabled(); reason != "" {
		return fmt.Sprintf("disabled: %s", reason)
	}
	return "active"
}

// warmTierEligible reports whether this container could sleep, which has to be
// answered before any process starts: os/exec decides who owns a child's pipe
// at start, and only a supervisor-owned pipe leaves a write end to hand CRIU at
// restore. Deciding late would mean deciding wrong.
func warmTierEligible() bool {
	if os.Getenv(upstreamEnv) == "" {
		return false
	}
	_, err := os.Stat(snapshot.SocketPath(""))
	return err == nil
}
