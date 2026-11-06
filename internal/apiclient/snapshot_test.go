package apiclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cboxdk/init/internal/snapshot"
)

func TestSnapshotClientActionsAndContext(t *testing.T) {
	for _, action := range []string{"status", "sleep", "wake"} {
		t.Run(action, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path, method := "/api/v1/snapshot/"+action, "POST"
				if action == "status" {
					path, method = "/api/v1/snapshot", "GET"
				}
				if r.URL.Path != path || r.Method != method || r.Header.Get("Authorization") != "Bearer token" || r.ContentLength != 0 {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"supported":true,"state":"asleep","idle_seconds":300}`))
			}))
			defer srv.Close()
			client := New(srv.URL, "token")
			call := client.Snapshot
			switch action {
			case "sleep":
				call = client.Sleep
			case "wake":
				call = client.Wake
			}
			status, err := call(context.Background())
			if err != nil || status.State != "asleep" || !status.Supported {
				t.Fatalf("%+v, %v", status, err)
			}
			if client.client.Timeout != 10*time.Second {
				t.Fatal("changed ordinary client timeout")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := call(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("context ignored: %v", err)
			}
		})
	}
}

func TestSnapshotClientPreservesRefusalAndDoesNotRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":"workload busy"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL, "").Sleep(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 409 || calls != 1 {
		t.Fatalf("calls %d, error %v", calls, err)
	}
	for _, action := range []func(context.Context) (snapshot.Status, error){(&Client{}).Sleep, (&Client{}).Snapshot} {
		if _, err := action(context.Background()); err == nil {
			t.Fatal("uninitialized client accepted")
		}
	}
}
