//go:build linux

package snapshot

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Runs only in a disposable privileged Linux test container, never on a node.
// The child PID namespace contains an untracked writer, standing in for a
// restore that completed before its acknowledgment was lost.
func TestUncertainOperationFencesPIDNamespace(t *testing.T) {
	if os.Getenv("CBOX_SNAPSHOT_KERNEL_TEST") != "1" {
		t.Skip("explicit disposable Linux namespace test only")
	}
	for _, operation := range []string{"CHECKPOINT", "RESTORE"} {
		for _, failure := range []string{"disconnect", "timeout"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				root := t.TempDir()
				socket := filepath.Join(root, "agent.sock")
				listener, err := net.Listen("unix", socket)
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				received := make(chan string, 1)
				proceed := make(chan struct{})
				done := make(chan struct{})
				defer close(done)
				go func() {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					defer conn.Close()
					line, _ := bufio.NewReader(conn).ReadString('\n')
					received <- strings.TrimSpace(line)
					select {
					case <-proceed:
					case <-done:
						return
					}
					if failure == "timeout" {
						<-done
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFenceNamespaceHelper$")
				cmd.Env = append(os.Environ(), "CBOX_FENCE_HELPER=init", "CBOX_FENCE_ROOT="+root, "CBOX_FENCE_OPERATION="+operation)
				cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWPID}
				var output bytes.Buffer
				cmd.Stdout, cmd.Stderr = &output, &output
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}()
				// Hold the same kind of namespace reference the runtime agent
				// keeps, so a dead PID namespace cannot be mistaken for a new one.
				namespace, err := os.Open("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/ns/pid")
				if err != nil {
					t.Fatal(err)
				}
				defer namespace.Close()
				select {
				case verb := <-received:
					if verb != operation {
						t.Fatalf("received %q", verb)
					}
				case <-ctx.Done():
					t.Fatal("helper did not send operation")
				}
				close(proceed)
				err = cmd.Wait()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 70 {
					t.Fatalf("PID 1 did not fence with exit 70: %v\n%s", err, output.String())
				}
				before, err := os.ReadFile(filepath.Join(root, "writes"))
				if err != nil || len(before) == 0 {
					t.Fatal("untracked writer never ran")
				}
				time.Sleep(100 * time.Millisecond)
				after, err := os.ReadFile(filepath.Join(root, "writes"))
				if err != nil || len(after) != len(before) {
					t.Fatal("untracked writer survived PID 1 fencing")
				}
				// A delayed agent still holding old namespace FDs must not be
				// able to fork a replacement into that dead namespace.
				marker := filepath.Join(root, "late-restore")
				late := exec.Command("nsenter", "--pid=/proc/self/fd/3", "--", "sh", "-c", "touch \"$1\"", "_", marker)
				late.ExtraFiles = []*os.File{namespace}
				if err := late.Run(); err == nil {
					t.Fatal("late operation entered the fenced PID namespace")
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("late operation executed after fencing")
				}
			})
		}
	}
}

func TestFenceNamespaceHelper(t *testing.T) {
	mode := os.Getenv("CBOX_FENCE_HELPER")
	if mode == "" {
		t.Skip("subprocess helper")
	}
	root := os.Getenv("CBOX_FENCE_ROOT")
	if mode == "writer" {
		file, err := os.OpenFile(filepath.Join(root, "writes"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		for {
			_, _ = file.WriteString("x")
			time.Sleep(10 * time.Millisecond)
		}
	}
	if os.Getpid() != 1 {
		t.Fatal("helper is not namespace PID 1")
	}
	child := exec.Command(os.Args[0], "-test.run=^TestFenceNamespaceHelper$")
	child.Env = append(os.Environ(), "CBOX_FENCE_HELPER=writer")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if info, err := os.Stat(filepath.Join(root, "writes")); err == nil && info.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer did not start")
		}
		time.Sleep(time.Millisecond)
	}
	client, err := Dial(filepath.Join(root, "agent.sock"))
	if err != nil {
		t.Fatal(err)
	}
	client.Timeout = 150 * time.Millisecond
	coordinator, proxy := newTestCoordinator(t, client, &fakeWorkload{})
	coordinator.fence = func(error) { os.Exit(70) }
	if os.Getenv("CBOX_FENCE_OPERATION") == "RESTORE" {
		_ = wakeAsleep(coordinator, proxy, context.Background())
	} else {
		coordinator.maybeSleep(context.Background())
	}
	t.Fatal("uncertain operation returned without fencing")
}
