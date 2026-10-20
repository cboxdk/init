package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// doc builds a minimal SBOM shaped like the parts normaliseRootVersion
// touches: the root component plus the dependency graph that repeats its
// bom-ref, and one real dependency that must be left alone.
func doc(rootPurl string, version any) map[string]any {
	component := map[string]any{
		"name":     "github.com/cboxdk/init",
		"bom-ref":  rootPurl,
		"purl":     rootPurl,
		"licenses": []any{"MIT"},
	}
	if version != nil {
		component["version"] = version
	}
	return map[string]any{
		"metadata": map[string]any{"component": component},
		"dependencies": []any{
			map[string]any{"ref": rootPurl},
			map[string]any{"ref": "pkg:golang/golang.org/x/net@v0.60.0?type=module"},
		},
	}
}

func rootRefs(t *testing.T, d map[string]any) (purl, bomRef, depRef string) {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Metadata struct {
			Component struct {
				Purl    string `json:"purl"`
				BomRef  string `json:"bom-ref"`
				Version string `json:"version"`
			} `json:"component"`
		} `json:"metadata"`
		Dependencies []struct {
			Ref string `json:"ref"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Metadata.Component.Version != rootVersion {
		t.Errorf("version = %q, want %q", got.Metadata.Component.Version, rootVersion)
	}
	return got.Metadata.Component.Purl, got.Metadata.Component.BomRef, got.Dependencies[0].Ref
}

func TestNormaliseRootVersion(t *testing.T) {
	const want = "pkg:golang/github.com/cboxdk/init@" + rootVersion + "?type=module"

	tests := []struct {
		name     string
		rootPurl string
		version  any
	}{
		{
			// The ordinary case: a checkout where git resolves a version.
			name:     "pseudo-version",
			rootPurl: "pkg:golang/github.com/cboxdk/init@v3.8.2-2-ge91c6bd?type=module",
			version:  "v3.8.2-2-ge91c6bd",
		},
		{
			// Inside a git worktree cyclonedx-gomod cannot resolve a version
			// and emits a version-less purl. Normalising only the `version`
			// field there produced an SBOM that differed from CI's.
			name:     "version-less purl",
			rootPurl: "pkg:golang/github.com/cboxdk/init?type=module",
			version:  nil,
		},
		{
			name:     "empty version string",
			rootPurl: "pkg:golang/github.com/cboxdk/init?type=module",
			version:  "",
		},
		{
			// Idempotent: re-running must not stack versions.
			name:     "already normalised",
			rootPurl: want,
			version:  rootVersion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := doc(tc.rootPurl, tc.version)
			normaliseRootVersion(d)

			purl, bomRef, depRef := rootRefs(t, d)
			for field, got := range map[string]string{"purl": purl, "bom-ref": bomRef, "dependencies[0].ref": depRef} {
				if got != want {
					t.Errorf("%s = %q, want %q", field, got, want)
				}
			}
		})
	}
}

// A dependency must not be rewritten just because the root is.
func TestNormaliseRootVersionLeavesDependencies(t *testing.T) {
	d := doc("pkg:golang/github.com/cboxdk/init?type=module", nil)
	normaliseRootVersion(d)

	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), "pkg:golang/golang.org/x/net@v0.60.0?type=module") {
		t.Errorf("dependency purl was rewritten: %s", raw)
	}
}

// A module path that merely starts with the root's name keeps its own version.
func TestNormaliseRootVersionDoesNotMatchLongerPath(t *testing.T) {
	d := doc("pkg:golang/github.com/cboxdk/init?type=module", nil)
	deps := d["dependencies"].([]any)
	d["dependencies"] = append(deps, map[string]any{
		"ref": "pkg:golang/github.com/cboxdk/init/tools@v1.0.0?type=module",
	})
	normaliseRootVersion(d)

	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), "pkg:golang/github.com/cboxdk/init/tools@v1.0.0?type=module") {
		t.Errorf("longer module path was rewritten: %s", raw)
	}
}
