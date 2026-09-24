package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/cboxdk/init/internal/config"
)

// Lines as postgres_exporter (Go log/slog, --log.format=json and the logfmt
// default) actually writes them. The JSON handler's keys are time, level,
// source and msg — "msg", not "message", and "time"/"level"/"msg" are exactly
// the keys cbox-init's own JSON output uses for the entry it re-emits.
const (
	exporterJSONLine   = `{"time":"2026-09-24T10:15:02.123456789Z","level":"INFO","source":"tls_config.go:347","msg":"Listening on","address":"127.0.0.1:9187"}`
	exporterJSONWarn   = `{"time":"2026-09-24T10:15:07.000Z","level":"WARN","source":"collector.go:210","msg":"collector failed","name":"stat_bgwriter","err":"pq: relation does not exist"}`
	exporterLogfmtLine = `time=2026-09-24T10:15:02.123Z level=INFO source=tls_config.go:347 msg="Listening on" address=127.0.0.1:9187`
)

// emitThroughWriter runs one line through a ProcessWriter wired to a JSON
// handler, the way cbox-init runs with log_format: json, and returns the raw
// line cbox-init wrote.
func emitThroughWriter(t *testing.T, jsonCfg *config.JSONConfig, line string) string {
	t.Helper()

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
		With("process", "postgres-exporter")

	pw, err := NewProcessWriter(log, "postgres-exporter", "postgres-exporter-0", "stderr", &config.LoggingConfig{
		Stdout: true, Stderr: true, JSON: jsonCfg,
	})
	if err != nil {
		t.Fatalf("NewProcessWriter: %v", err)
	}
	if _, err := pw.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if strings.Count(out, "\n") != 0 {
		t.Fatalf("expected one output line, got:\n%s", out)
	}

	return out
}

// duplicateKeys reports every top-level key that appears more than once in a
// JSON object. encoding/json silently keeps the last one, which is exactly how
// the duplication went unnoticed — and why a log pipeline that does the same
// ended up with the child's "msg" and "time" in place of cbox-init's.
func duplicateKeys(t *testing.T, line string) []string {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(line))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("output is not a JSON object: %s", line)
	}

	seen := map[string]int{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("decode key: %v", err)
		}
		seen[tok.(string)]++

		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil && err != io.EOF {
			t.Fatalf("decode value: %v", err)
		}
	}

	var dups []string
	for key, n := range seen {
		if n > 1 {
			dups = append(dups, fmt.Sprintf("%s×%d", key, n))
		}
	}

	return dups
}

func decodeLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("decode %s: %v", line, err)
	}
	return m
}

func allJSONOptions() *config.JSONConfig {
	return &config.JSONConfig{Enabled: true, DetectAuto: true, ExtractLevel: true, ExtractMessage: true, MergeFields: true}
}

// The bug: with every option on, a postgres_exporter line came out with two
// "time" keys and two "msg" keys, and the message was the whole raw JSON line,
// because only a field called "message" was ever lifted.
func TestJSONLogs_ExporterLineHasNoDuplicateKeys(t *testing.T) {
	out := emitThroughWriter(t, allJSONOptions(), exporterJSONLine)

	if dups := duplicateKeys(t, out); len(dups) > 0 {
		t.Errorf("duplicate keys %v in:\n%s", dups, out)
	}

	m := decodeLine(t, out)
	if m["msg"] != "Listening on" {
		t.Errorf("msg = %q, want the child's own message %q", m["msg"], "Listening on")
	}
	if m["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", m["level"])
	}
	// The child's timestamp is kept, not dropped — under a name that does not
	// collide with cbox-init's own.
	if m["app_time"] != "2026-09-24T10:15:02.123456789Z" {
		t.Errorf("app_time = %v, want the child's timestamp", m["app_time"])
	}
	for key, want := range map[string]string{
		"source": "tls_config.go:347", "address": "127.0.0.1:9187",
		"process": "postgres-exporter", "instance_id": "postgres-exporter-0", "stream": "stderr",
	} {
		if m[key] != want {
			t.Errorf("%s = %v, want %q", key, m[key], want)
		}
	}
}

func TestJSONLogs_ExporterWarnKeepsItsLevel(t *testing.T) {
	out := emitThroughWriter(t, allJSONOptions(), exporterJSONWarn)
	if dups := duplicateKeys(t, out); len(dups) > 0 {
		t.Errorf("duplicate keys %v in:\n%s", dups, out)
	}
	m := decodeLine(t, out)
	if m["level"] != "WARN" || m["msg"] != "collector failed" || m["err"] != "pq: relation does not exist" {
		t.Errorf("unexpected entry: %s", out)
	}
}

// With nothing lifted, every field is merged — and every one that collides
// with a key cbox-init writes itself is renamed rather than duplicated.
func TestJSONLogs_MergeOnlyRenamesEveryCollision(t *testing.T) {
	out := emitThroughWriter(t, &config.JSONConfig{Enabled: true, DetectAuto: true, MergeFields: true},
		`{"time":"t0","level":"debug","msg":"m","process":"p","instance_id":"i","stream":"s","other":"o"}`)

	if dups := duplicateKeys(t, out); len(dups) > 0 {
		t.Errorf("duplicate keys %v in:\n%s", dups, out)
	}
	m := decodeLine(t, out)
	for _, key := range []string{"time", "level", "msg", "process", "instance_id", "stream"} {
		if _, ok := m["app_"+key]; !ok {
			t.Errorf("colliding key %q was not kept as app_%s: %s", key, key, out)
		}
	}
	if m["other"] != "o" {
		t.Errorf("non-colliding key renamed or lost: %s", out)
	}
	if m["process"] != "postgres-exporter" || m["stream"] != "stderr" {
		t.Errorf("cbox-init's own fields were overwritten by the child's: %s", out)
	}
}

// A prefixed name that is itself taken must not collide either.
func TestJSONLogs_RenameDoesNotCollideWithExistingPrefixedKey(t *testing.T) {
	out := emitThroughWriter(t, &config.JSONConfig{Enabled: true, MergeFields: true},
		`{"time":"a","app_time":"b"}`)

	if dups := duplicateKeys(t, out); len(dups) > 0 {
		t.Errorf("duplicate keys %v in:\n%s", dups, out)
	}
	m := decodeLine(t, out)
	if m["app_time"] != "b" || m["app_app_time"] != "a" {
		t.Errorf("want app_time=b (the child's own) and app_app_time=a (its renamed time): %s", out)
	}
}

func TestJSONParser_MessageField(t *testing.T) {
	cases := []struct {
		name  string
		field string
		data  map[string]any
		want  string
		kept  []string // keys that must remain as attributes
	}{
		{"default lifts message", "", map[string]any{"message": "a"}, "a", nil},
		{"default falls back to msg", "", map[string]any{"msg": "b"}, "b", nil},
		{"default prefers message over msg", "", map[string]any{"message": "a", "msg": "b"}, "a", []string{"msg"}},
		{"configured msg", "msg", map[string]any{"msg": "b", "message": "a"}, "b", []string{"message"}},
		{"configured custom key", "event", map[string]any{"event": "e", "msg": "b"}, "e", []string{"msg"}},
		{"configured key absent lifts nothing", "event", map[string]any{"msg": "b"}, "", []string{"msg"}},
		{"non-string is not lifted", "msg", map[string]any{"msg": 42}, "", []string{"msg"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewJSONParser(&config.JSONConfig{Enabled: true, ExtractMessage: true, MergeFields: true, MessageField: tc.field})
			msg, _, attrs, _ := p.ToLogAttrs(tc.data)
			if msg != tc.want {
				t.Errorf("message = %q, want %q", msg, tc.want)
			}
			if len(attrs) != len(tc.kept) {
				t.Errorf("attrs = %v, want exactly the keys %v", attrs, tc.kept)
			}
		})
	}
}

func TestJSONParser_LevelField(t *testing.T) {
	p := NewJSONParser(&config.JSONConfig{Enabled: true, ExtractLevel: true, LevelField: "severity"})
	_, level, _, found := p.ToLogAttrs(map[string]any{"severity": "ERROR", "level": "debug"})
	if !found || level != slog.LevelError {
		t.Errorf("level = %v (found=%v), want ERROR from the configured severity field", level, found)
	}

	p = NewJSONParser(&config.JSONConfig{Enabled: true, ExtractLevel: true})
	_, level, _, found = p.ToLogAttrs(map[string]any{"level": "WARN"})
	if !found || level != slog.LevelWarn {
		t.Errorf("default level field: level = %v (found=%v), want WARN", level, found)
	}
}

// Merged fields come out in a stable order, so two identical lines produce
// identical output and diffs of captured logs mean something.
func TestJSONParser_MergedFieldsAreSorted(t *testing.T) {
	p := NewJSONParser(&config.JSONConfig{Enabled: true, MergeFields: true})
	_, _, attrs, _ := p.ToLogAttrs(map[string]any{"zeta": 1, "alpha": 2, "mid": 3})
	var keys []string
	for _, a := range attrs {
		keys = append(keys, a.Key)
	}
	if strings.Join(keys, ",") != "alpha,mid,zeta" {
		t.Errorf("merged keys in order %v, want alpha,mid,zeta", keys)
	}
}

// logfmt is not JSON. With detect_auto the line must pass through untouched —
// not half-parsed, not dropped — and level detection, if configured, still
// sees it.
func TestJSONLogs_LogfmtPassesThroughIntact(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	pw, err := NewProcessWriter(log, "postgres-exporter", "postgres-exporter-0", "stderr", &config.LoggingConfig{
		Stdout: true, Stderr: true,
		JSON: allJSONOptions(),
		LevelDetection: &config.LevelDetectionConfig{
			Enabled:  true,
			Patterns: map[string]string{"warn": `\blevel=WARN\b`, "error": `\blevel=ERROR\b`},
		},
	})
	if err != nil {
		t.Fatalf("NewProcessWriter: %v", err)
	}

	warn := strings.Replace(exporterLogfmtLine, "level=INFO", "level=WARN", 1)
	for _, line := range []string{exporterLogfmtLine, warn} {
		if _, err := pw.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 output lines, got %d:\n%s", len(lines), buf.String())
	}

	first, second := decodeLine(t, lines[0]), decodeLine(t, lines[1])
	if first["msg"] != exporterLogfmtLine {
		t.Errorf("logfmt line altered: msg = %q", first["msg"])
	}
	if first["level"] != "INFO" || second["level"] != "WARN" {
		t.Errorf("levels = %v, %v; want INFO, WARN from level detection", first["level"], second["level"])
	}
	for _, line := range lines {
		if dups := duplicateKeys(t, line); len(dups) > 0 {
			t.Errorf("duplicate keys %v in:\n%s", dups, line)
		}
	}
}
