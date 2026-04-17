package apiclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cboxdk/init/internal/logger"
)

// StreamLogs connects to the SSE log stream and returns a channel of log entries.
// The channel is closed when the context is cancelled or the connection drops.
// If process is non-empty, only logs from that process are streamed.
func (c *Client) StreamLogs(ctx context.Context, process string) (<-chan logger.LogEntry, error) {
	path := "/api/v1/logs/stream"
	if process != "" {
		path = fmt.Sprintf("%s?process=%s", path, process)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.getURL(path), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	if c.auth != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.auth))
	}

	// Use a client without timeout for streaming
	streamClient := *c.client
	streamClient.Timeout = 0

	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to log stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("log stream returned status %d", resp.StatusCode)
	}

	ch := make(chan logger.LogEntry, 256)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				var entry struct {
					Timestamp string `json:"timestamp"`
					Process   string `json:"process"`
					Instance  string `json:"instance"`
					Stream    string `json:"stream"`
					Level     string `json:"level"`
					Message   string `json:"message"`
				}
				if err := json.Unmarshal([]byte(data), &entry); err != nil {
					continue
				}

				ts, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)

				logEntry := logger.LogEntry{
					Timestamp:   ts,
					ProcessName: entry.Process,
					InstanceID:  entry.Instance,
					Stream:      entry.Stream,
					Level:       entry.Level,
					Message:     entry.Message,
				}

				select {
				case ch <- logEntry:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}
