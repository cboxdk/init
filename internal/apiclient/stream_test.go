package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamLogs_ReceivesEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/logs/stream" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer is not a flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, "event: log\ndata: {\"timestamp\":\"2026-04-17T10:00:00Z\",\"process\":\"nginx\",\"level\":\"info\",\"message\":\"hello\"}\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "event: log\ndata: {\"timestamp\":\"2026-04-17T10:00:01Z\",\"process\":\"nginx\",\"level\":\"info\",\"message\":\"world\"}\n\n")
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer server.Close()

	client := New(server.URL, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.StreamLogs(ctx, "")
	if err != nil {
		t.Fatalf("StreamLogs error: %v", err)
	}

	entry1 := <-ch
	if entry1.Message != "hello" || entry1.ProcessName != "nginx" {
		t.Errorf("unexpected first entry: %+v", entry1)
	}

	entry2 := <-ch
	if entry2.Message != "world" {
		t.Errorf("unexpected second entry: %+v", entry2)
	}
}

func TestStreamLogs_WithProcessFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		process := r.URL.Query().Get("process")
		if process != "nginx" {
			t.Errorf("expected process=nginx, got %q", process)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: log\ndata: {\"process\":\"nginx\",\"message\":\"filtered\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	client := New(server.URL, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.StreamLogs(ctx, "nginx")
	if err != nil {
		t.Fatalf("StreamLogs error: %v", err)
	}

	entry := <-ch
	if entry.Message != "filtered" {
		t.Errorf("unexpected: %+v", entry)
	}
}

func TestStreamLogs_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	client := New(server.URL, "")
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := client.StreamLogs(ctx, "")
	if err != nil {
		t.Fatalf("StreamLogs error: %v", err)
	}

	cancel()

	select {
	case <-ch:
		// Channel delivered or closed — both are acceptable after cancel
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after context cancel")
	}
}
