package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cboxdk/init/internal/snapshot"
)

// Snapshot reads the runtime without waking it or resetting its idle timer.
func (c *Client) Snapshot(ctx context.Context) (snapshot.Status, error) {
	var status snapshot.Status
	err := c.do(ctx, http.MethodGet, "/api/v1/snapshot", nil, &status)
	return status, err
}

// Sleep requests a safe checkpoint, without waiting for the idle timer.
func (c *Client) Sleep(ctx context.Context) (snapshot.Status, error) {
	return c.snapshotAction(ctx, "sleep")
}

// Wake restores a checkpoint through the coordinator's ordinary recovery path.
func (c *Client) Wake(ctx context.Context) (snapshot.Status, error) {
	return c.snapshotAction(ctx, "wake")
}

func (c *Client) snapshotAction(ctx context.Context, action string) (snapshot.Status, error) {
	var status snapshot.Status
	if c.client == nil {
		return status, fmt.Errorf("API client not initialized")
	}
	// Do not change shared client state or the deadlines for ordinary requests.
	client := *c
	transport := *c.client
	transport.Timeout = 75 * time.Second
	client.client = &transport
	err := client.do(ctx, http.MethodPost, "/api/v1/snapshot/"+action, nil, &status)
	return status, err
}
