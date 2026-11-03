package snapshot

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRegisterRequiresStderrAcknowledgment(t *testing.T) {
	for _, supported := range []bool{true, false} {
		t.Run(fmt.Sprint(supported), func(t *testing.T) {
			root, err := os.MkdirTemp("/tmp", "init-stdio-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "s"), Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			out, err := NewStream(io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			stderr, err := NewStream(io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				buf, oob := make([]byte, 512), make([]byte, syscall.CmsgSpace(8))
				n, on, _, _, err := conn.ReadMsgUnix(buf, oob)
				if err != nil {
					done <- err
					return
				}
				messages, err := syscall.ParseSocketControlMessage(oob[:on])
				if err != nil {
					done <- err
					return
				}
				var fds []int
				for _, m := range messages {
					rights, e := syscall.ParseUnixRights(&m)
					if e != nil {
						done <- e
						return
					}
					fds = append(fds, rights...)
				}
				defer func() {
					for _, fd := range fds {
						_ = syscall.Close(fd)
					}
				}()
				if len(fds) != 2 {
					done <- fmt.Errorf("got %d descriptors", len(fds))
					return
				}
				msg, err := ParseMessage(string(buf[:n]))
				if err != nil {
					done <- err
					return
				}
				for i, key := range []string{"inode", "stderr_inode"} {
					var stat syscall.Stat_t
					if err := syscall.Fstat(fds[i], &stat); err != nil || fmt.Sprint(stat.Ino) != msg.Fields[key] {
						done <- fmt.Errorf("wrong %s descriptor", key)
						return
					}
				}
				reply := "OK port=9001 idle=15"
				if supported {
					reply += " stdio=2"
				}
				_, err = fmt.Fprintln(conn, reply)
				done <- err
			}()
			client, err := Dial(listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_, err = client.Register(context.Background(), 42, out, stderr)
			if supported && err != nil {
				t.Fatal(err)
			}
			if !supported && err == nil {
				t.Fatal("old agent accepted without stderr acknowledgment")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
