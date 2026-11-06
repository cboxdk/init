package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSnapshotCommandUsesNativeAPIAndPrintsState(t *testing.T) {
	for _, action := range []string{"status", "sleep", "wake"} {
		t.Run(action, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path, method := "/api/v1/snapshot/"+action, "POST"
				if action == "status" {
					path, method = "/api/v1/snapshot", "GET"
				}
				if r.URL.Path != path || r.Method != method {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"supported":true,"state":"asleep","idle_seconds":300}`))
			}))
			defer srv.Close()
			out, ok := runCLI(t, srv.URL, "snapshot", action)
			if !ok || !strings.Contains(out, `"state":"asleep"`) {
				t.Fatalf("exit success %v: %s", ok, out)
			}
		})
	}
}

func TestSnapshotCommandRefusalExitsNonzero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":"workload busy"}`))
	}))
	defer srv.Close()
	for _, args := range [][]string{{"snapshot", "sleep"}, {"snapshot", "force"}, {"snapshot"}, {"snapshot", "sleep", "force"}} {
		if out, ok := runCLI(t, srv.URL, args...); ok {
			t.Fatalf("accepted %v: %s", args, out)
		}
	}
}
