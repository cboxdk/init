// Command sbomnorm makes a cyclonedx-gomod SBOM reproducible.
//
// Three things in the generated document depend on where and when it was
// generated rather than on the dependency set, which would make the committed
// SBOM drift on every commit and on every developer machine:
//
//   - the root component's version is a git pseudo-version derived from HEAD,
//     or missing entirely where git cannot resolve one
//   - purls carry the generating host's goos/goarch qualifiers
//   - metadata.tools records hashes of the cyclonedx-gomod binary, which differ
//     per platform and per build of the tool
//
// All three are normalised here, so `make sbom` is a no-op unless the
// dependencies actually changed.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const rootVersion = "0.0.0"

var purlPattern = regexp.MustCompile(`pkg:golang/[^"]+`)

func main() {
	path := "sbom.json"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbomnorm: %v\n", err)
		os.Exit(1)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "sbomnorm: failed to parse %s: %v\n", path, err)
		os.Exit(1)
	}

	normaliseRootVersion(doc)
	dropToolHashes(doc)

	out, err := marshal(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbomnorm: %v\n", err)
		os.Exit(1)
	}

	// The purls are plain strings all over the document (components,
	// bom-refs, the dependency graph), so strip the platform qualifiers
	// textually rather than walking every shape.
	out = []byte(stripPlatformQualifiers(string(out)))

	if err := os.WriteFile(path, out, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "sbomnorm: %v\n", err)
		os.Exit(1)
	}
}

// normaliseRootVersion replaces the pseudo-version of the module being
// described. It changes with every commit and says nothing about dependencies.
func normaliseRootVersion(doc map[string]any) {
	metadata, ok := doc["metadata"].(map[string]any)
	if !ok {
		return
	}

	component, ok := metadata["component"].(map[string]any)
	if !ok {
		return
	}

	component["version"] = rootVersion

	name, _ := component["name"].(string)
	if name == "" {
		return
	}

	// The version also appears inside the root purl and bom-ref, and it may be
	// absent there: cyclonedx-gomod cannot resolve a version for the main
	// module in every checkout (inside a git worktree it warns "failed to
	// determine version of main module" and emits a version-less purl), and
	// normalising only the `version` field would then leave a document that
	// differs from the one CI generates -- which passes `sbom-check` locally
	// and fails it in CI. Pin the purl to rootVersion in both cases.
	//
	// The trailing `?` or `"` is what keeps this off a longer module path that
	// merely starts with the root's name.
	rootPurl := regexp.MustCompile(`pkg:golang/` + regexp.QuoteMeta(name) + `(?:@[^"?]*)?([?"])`)
	replacement := "pkg:golang/" + name + "@" + rootVersion + "$1"

	if raw, err := json.Marshal(doc); err == nil {
		var replaced map[string]any
		if err := json.Unmarshal(rootPurl.ReplaceAll(raw, []byte(replacement)), &replaced); err == nil {
			for k, v := range replaced {
				doc[k] = v
			}
		}
	}
}

// dropToolHashes removes the hashes of the generating tool's own binary. The
// tool's name and version stay, which is the part that carries meaning.
func dropToolHashes(doc map[string]any) {
	metadata, ok := doc["metadata"].(map[string]any)
	if !ok {
		return
	}

	switch tools := metadata["tools"].(type) {
	case []any: // CycloneDX <= 1.4 shape, still emitted for 1.5
		for _, tool := range tools {
			if entry, ok := tool.(map[string]any); ok {
				delete(entry, "hashes")
			}
		}
	case map[string]any: // 1.5+ shape: tools.components[]
		if components, ok := tools["components"].([]any); ok {
			for _, component := range components {
				if entry, ok := component.(map[string]any); ok {
					delete(entry, "hashes")
				}
			}
		}
	}
}

// stripPlatformQualifiers rewrites every purl in the document, dropping the
// goos and goarch qualifiers and leaving the rest of the purl intact.
func stripPlatformQualifiers(s string) string {
	return purlPattern.ReplaceAllStringFunc(s, func(purl string) string {
		name, qualifiers, found := strings.Cut(purl, "?")
		if !found {
			return purl
		}

		var kept []string
		for _, qualifier := range strings.Split(qualifiers, "&") {
			if strings.HasPrefix(qualifier, "goos=") || strings.HasPrefix(qualifier, "goarch=") {
				continue
			}
			kept = append(kept, qualifier)
		}

		if len(kept) == 0 {
			return name
		}

		return name + "?" + strings.Join(kept, "&")
	})
}

func marshal(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
