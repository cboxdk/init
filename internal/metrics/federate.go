package metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Federation limits. A local exporter that suddenly returns hundreds of
// megabytes should degrade to "down", not balloon every scrape of cbox-init.
const (
	federateMaxBodyBytes    = 8 << 20 // 8 MiB per source
	federateDefaultTimeout  = 2 * time.Second
	federateDefaultCacheTTL = 5 * time.Second
)

// FederateSource is one local exporter merged into the main /metrics response.
type FederateSource struct {
	Name     string
	URL      string
	Timeout  time.Duration
	CacheTTL time.Duration
}

type federateEntry struct {
	mu        sync.Mutex
	body      []byte
	fetchedAt time.Time
	up        bool
}

// Federator fetches declared local exporters and appends their exposition to
// the main metrics response. Each source is cached for its TTL so a busy
// Prometheus (or several) does not multiply load onto small exporters, and a
// source that is down contributes cbox_init_federate_up{name} 0 instead of
// failing the scrape.
type Federator struct {
	sources []FederateSource
	entries []*federateEntry
	client  *http.Client
	logger  *slog.Logger
}

// NewFederator builds a federator over the declared sources. Defaults are
// applied here (not in config) so the zero-value config stays honest.
func NewFederator(sources []FederateSource, logger *slog.Logger) *Federator {
	entries := make([]*federateEntry, len(sources))
	maxTimeout := time.Duration(0)
	for i := range sources {
		if sources[i].Timeout <= 0 {
			sources[i].Timeout = federateDefaultTimeout
		}
		if sources[i].CacheTTL <= 0 {
			sources[i].CacheTTL = federateDefaultCacheTTL
		}
		if sources[i].Timeout > maxTimeout {
			maxTimeout = sources[i].Timeout
		}
		entries[i] = &federateEntry{}
	}

	return &Federator{
		sources: sources,
		entries: entries,
		// The client timeout is a backstop; the per-request context carries
		// each source's own timeout.
		client: &http.Client{Timeout: maxTimeout + time.Second},
		logger: logger,
	}
}

// Append writes every source's exposition (cached or freshly fetched) followed
// by the cbox_init_federate_up block. It never returns an error for a source
// being down — that is what the gauge is for.
func (f *Federator) Append(ctx context.Context, w io.Writer) {
	up := make([]bool, len(f.sources))
	for i := range f.sources {
		body, ok := f.fetch(ctx, i)
		up[i] = ok
		if !ok || len(body) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n# Federated from %s (%s)\n", f.sources[i].Name, f.sources[i].URL)
		_, _ = w.Write(body)
		if body[len(body)-1] != '\n' {
			_, _ = io.WriteString(w, "\n")
		}
	}

	_, _ = io.WriteString(w, "\n# HELP cbox_init_federate_up Whether the federated metrics source responded on the last fetch (1) or is being skipped as down (0).\n")
	_, _ = io.WriteString(w, "# TYPE cbox_init_federate_up gauge\n")
	for i, src := range f.sources {
		v := 0
		if up[i] {
			v = 1
		}
		fmt.Fprintf(w, "cbox_init_federate_up{name=%q} %d\n", src.Name, v)
	}
}

// fetch returns the cached body when fresh, otherwise fetches. A failed fetch
// marks the source down and drops the cached body: stale metrics presented as
// current are worse than an honest gap plus federate_up 0.
func (f *Federator) fetch(ctx context.Context, i int) ([]byte, bool) {
	src := f.sources[i]
	e := f.entries[i]

	e.mu.Lock()
	defer e.mu.Unlock()

	if time.Since(e.fetchedAt) < src.CacheTTL {
		return e.body, e.up
	}

	reqCtx, cancel := context.WithTimeout(ctx, src.Timeout)
	defer cancel()

	body, err := f.fetchOnce(reqCtx, src.URL)
	e.fetchedAt = time.Now()
	if err != nil {
		if e.up { // log the transition, not every scrape
			f.logger.Warn("Federated metrics source is down", "name", src.Name, "error", err)
		}
		e.up = false
		e.body = nil
		return nil, false
	}
	if !e.up && e.fetchedAt != (time.Time{}) {
		f.logger.Info("Federated metrics source is up", "name", src.Name)
	}
	e.up = true
	e.body = body
	return body, true
}

func (f *Federator) fetchOnce(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Ask for the plain text exposition; the response is appended verbatim.
	req.Header.Set("Accept", "text/plain;version=0.0.4")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, federateMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > federateMaxBodyBytes {
		return nil, fmt.Errorf("body exceeds %d bytes", federateMaxBodyBytes)
	}

	return sanitizeExposition(raw), nil
}

// sanitizeExposition drops OpenMetrics terminators — "# EOF" in the middle of
// a concatenated response would truncate parsing for some scrapers.
func sanitizeExposition(raw []byte) []byte {
	if !bytes.Contains(raw, []byte("# EOF")) {
		return raw
	}
	var b bytes.Buffer
	b.Grow(len(raw))
	for _, line := range strings.SplitAfter(string(raw), "\n") {
		if strings.TrimSpace(line) == "# EOF" {
			continue
		}
		b.WriteString(line)
	}
	return b.Bytes()
}
