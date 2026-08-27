package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Config loading uses KnownFields(true), so an unrecognised key is a hard
// failure for `serve` as well as `check-config`. A documented key that does not
// exist therefore does not merely mislead — an operator who copies the snippet
// gets a container that will not start.
//
// The repo already gates configs/examples/*.yaml (tools/check-doc-configs.sh),
// but nothing checked the YAML embedded in docs/, which is where most readers
// actually copy from. Nine nonexistent keys had accumulated there:
// restart_delay, max_restart_delay, max_restarts, restart_threshold, a
// per-process dependency_timeout, shutdown.post_stop_hook, and api_acl.allow /
// api_acl.deny.
var (
	yamlFence = regexp.MustCompile("(?s)```yaml\n(.*?)```")

	// Blocks that are cbox-init configs rather than compose files, Prometheus
	// scrape configs, OTel collector configs, and so on.
	looksLikeConfig = regexp.MustCompile(`(?m)^(version:|global:|processes:|hooks:)`)
	notOurConfig    = regexp.MustCompile(`(?m)^(services:|volumes:|networks:|receivers:|exporters:|processors:|scrape_configs:|apiVersion:)`)

	// Anchored at column 0: `processes:` also appears indented under
	// global.readiness, and a nested match would skip the placeholder that makes
	// a partial snippet loadable.
	topLevelVersion   = regexp.MustCompile(`(?m)^version:`)
	topLevelProcesses = regexp.MustCompile(`(?m)^processes:`)
)

// deliberatelyInvalid lists snippets that document a REJECTED config — the docs
// use them to show what the error looks like. Keyed by "path:line-of-fence".
var deliberatelyInvalid = map[string]string{
	"docs/features/dev-mode.md": "scale: abc",
}

func TestDocumentedConfigSnippetsLoad(t *testing.T) {
	root := filepath.Join("..", "..", "docs")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("docs tree not present: %v", err)
	}

	checked := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel := filepath.ToSlash(path)
		rel = strings.TrimPrefix(rel, "../../")

		for _, m := range yamlFence.FindAllStringSubmatchIndex(string(body), -1) {
			block := string(body[m[2]:m[3]])
			if !looksLikeConfig.MatchString(block) || notOurConfig.MatchString(block) {
				continue
			}
			if marker, ok := deliberatelyInvalid[rel]; ok && strings.Contains(block, marker) {
				continue
			}

			line := strings.Count(string(body[:m[0]]), "\n") + 1
			checked++

			// Only schema faults are asserted. A snippet that shows just the
			// health_check block for a process, without repeating its command,
			// fails structural validation — that is ordinary documentation
			// style, and enforcing it would push the docs toward noise. An
			// unknown key or a mistyped value is different: the reader cannot
			// see it, and KnownFields(true) makes it fatal for `serve` too.
			if err := loadSnippet(t, block); err != nil && isSchemaFault(err) {
				t.Errorf("%s:%d — documented config uses a key or type the binary rejects:\n  %v\n%s",
					rel, line, err, indent(block))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking docs: %v", err)
	}

	if checked == 0 {
		t.Fatal("no config snippets found; the fence or filter regexes have drifted")
	}
	t.Logf("checked %d documented config snippets", checked)
}

// loadSnippet runs one documentation fence through the real loader. Partial
// snippets (a `global:` block on its own, say) are completed with the minimum
// needed to be a valid config, so the test measures the keys the snippet
// actually uses rather than what it leaves out.
func loadSnippet(t *testing.T, block string) error {
	t.Helper()

	cfg := block
	if !topLevelVersion.MatchString(cfg) {
		cfg = "version: \"1.0\"\n" + cfg
	}
	if !topLevelProcesses.MatchString(cfg) {
		cfg += "\nprocesses:\n  placeholder:\n    command: [\"/bin/true\"]\n"
	}

	path := filepath.Join(t.TempDir(), "cbox-init.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		return err
	}

	_, err := LoadWithEnvExpansion(path)

	return err
}

// isSchemaFault distinguishes "this key does not exist / has the wrong type"
// from "this snippet is abbreviated".
func isSchemaFault(err error) bool {
	msg := err.Error()

	return strings.Contains(msg, "not found in type") ||
		strings.Contains(msg, "cannot unmarshal")
}

func indent(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("      " + line + "\n")
	}
	return b.String()
}
