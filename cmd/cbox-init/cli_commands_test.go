package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCLISubprocess is the subprocess entrypoint for the CLI command tests
// below. Because the control commands (list/restart/…) call os.Exit directly,
// they must run in their own process; the parent test captures the output and
// exit code. It runs the CLI with the args in CBOX_CLI_ARGS.
func TestCLISubprocess(t *testing.T) {
	if os.Getenv("BE_CLI_SUBPROC") != "1" {
		return
	}
	rootCmd.SetArgs(strings.Fields(os.Getenv("CBOX_CLI_ARGS")))
	_ = rootCmd.Execute()
}

// runCLI runs the CLI in a subprocess against srvURL and returns its combined
// output and whether it exited zero.
func runCLI(t *testing.T, srvURL string, args ...string) (string, bool) {
	t.Helper()
	full := append(args, "--url", srvURL)
	cmd := exec.Command(os.Args[0], "-test.run=TestCLISubprocess")
	cmd.Env = append(os.Environ(), "BE_CLI_SUBPROC=1", "CBOX_CLI_ARGS="+strings.Join(full, " "))
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func TestListCommand_AgainstServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/processes" {
			_, _ = w.Write([]byte(`{"processes":[{"name":"web","state":"running","scale":2,"desired_scale":2,"instances":[{"started_at":1700000000,"restart_count":1}]}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	out, ok := runCLI(t, srv.URL, "list")
	if !ok {
		t.Fatalf("list exited non-zero for an all-running stack:\n%s", out)
	}
	if !strings.Contains(out, "web") || !strings.Contains(out, "running") {
		t.Errorf("list output missing the process row:\n%s", out)
	}
}

func TestListCommand_UnhealthyExitsNonZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"processes":[{"name":"web","state":"failed","scale":1,"desired_scale":1,"instances":[]}]}`))
	}))
	defer srv.Close()

	out, ok := runCLI(t, srv.URL, "list")
	if ok {
		t.Errorf("list should exit non-zero when a process is unhealthy; output:\n%s", out)
	}
}

func TestRestartCommand_AgainstServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/restart") {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	out, ok := runCLI(t, srv.URL, "restart", "web")
	if !ok {
		t.Fatalf("restart exited non-zero on a 200 response:\n%s", out)
	}
	if !strings.Contains(out, "restarted") {
		t.Errorf("restart output missing confirmation:\n%s", out)
	}
}

func TestRestartCommand_NotFoundExitsNonZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"process not found"}`))
	}))
	defer srv.Close()

	out, ok := runCLI(t, srv.URL, "restart", "ghost")
	if ok {
		t.Errorf("restart should exit non-zero when the process is not found; output:\n%s", out)
	}
	if !strings.Contains(out, "Failed to restart") {
		t.Errorf("restart error output missing:\n%s", out)
	}
}
