// Command licensecheck fails the build when a dependency is not offered under a
// permissive license.
//
// It reads the committed CycloneDX SBOM rather than the network, so it produces
// the same answer in CI as it does offline. Regenerate the SBOM first (`make
// sbom`); a stale SBOM is caught separately by the drift check in CI.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
)

// Permissive licenses. A component passes if ANY of its licenses is on this
// list, which is how SPDX dual-licensing works: "BSD-3-Clause OR GPL-3.0-only"
// is fine, because we take it under BSD.
var permissive = []string{
	"0BSD",
	"Apache-2.0",
	"BSD-2-Clause",
	"BSD-3-Clause",
	"BSD-Source-Code",
	"ISC",
	"MIT",
	"MIT-0",
	"Unlicense",
	"Zlib",
}

type exception struct {
	license string
	reason  string
}

// Licenses accepted for one named component only, each with the reason it is
// acceptable. Anything not listed here has to be permissive.
var exceptions = map[string]exception{
	"github.com/shoenig/go-m1cpu": {
		license: "MPL-2.0",
		reason: "darwin/arm64-only (//go:build darwin && arm64 && cgo), pulled " +
			"transitively through gopsutil for Apple Silicon CPU detection and never " +
			"compiled into the Linux production binary. MPL-2.0 is file-level weak " +
			"copyleft: linking it imposes nothing on our code, and we do not modify it.",
	},
}

type license struct {
	License struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"license"`
	Expression string `json:"expression"`
}

type sbom struct {
	Components []struct {
		Name     string    `json:"name"`
		Licenses []license `json:"licenses"`
	} `json:"components"`
}

func main() {
	path := "sbom.json"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "licensecheck: %v\n", err)
		fmt.Fprintf(os.Stderr, "licensecheck: run `make sbom` first\n")
		os.Exit(1)
	}

	var doc sbom
	if err := json.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "licensecheck: failed to parse %s: %v\n", path, err)
		os.Exit(1)
	}

	if len(doc.Components) == 0 {
		fmt.Fprintf(os.Stderr, "licensecheck: %s lists no components\n", path)
		os.Exit(1)
	}

	var problems, excepted []string

	for _, component := range doc.Components {
		ids := licenseIDs(component.Licenses)

		if len(ids) == 0 {
			problems = append(problems, fmt.Sprintf("%s: no license detected", component.Name))
			continue
		}

		if slices.ContainsFunc(ids, func(id string) bool { return slices.Contains(permissive, id) }) {
			continue
		}

		if allowed, ok := exceptions[component.Name]; ok && slices.Contains(ids, allowed.license) {
			excepted = append(excepted, fmt.Sprintf("%s (%s): %s", component.Name, allowed.license, allowed.reason))
			continue
		}

		problems = append(problems, fmt.Sprintf("%s: %s is not permissive", component.Name, strings.Join(ids, ", ")))
	}

	sort.Strings(problems)
	for _, problem := range problems {
		fmt.Fprintf(os.Stderr, "licensecheck: %s\n", problem)
	}

	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "\nlicensecheck: %d dependencies need review. Either add a justified\n", len(problems))
		fmt.Fprintf(os.Stderr, "exception in tools/licensecheck/main.go, or drop the dependency.\n")
		os.Exit(1)
	}

	for _, note := range excepted {
		fmt.Printf("licensecheck: accepted under a documented exception: %s\n", note)
	}

	fmt.Printf("licensecheck: %d dependencies, all permissive (%d under a documented exception)\n",
		len(doc.Components), len(excepted))
}

// licenseIDs flattens the CycloneDX license shapes into plain SPDX identifiers.
func licenseIDs(licenses []license) []string {
	var ids []string

	for _, entry := range licenses {
		switch {
		case entry.License.ID != "":
			ids = append(ids, entry.License.ID)
		case entry.License.Name != "":
			ids = append(ids, entry.License.Name)
		case entry.Expression != "":
			// "MIT OR Apache-2.0" — split so any permissive branch passes.
			for _, part := range strings.Split(entry.Expression, " OR ") {
				ids = append(ids, strings.Trim(part, "() "))
			}
		}
	}

	return ids
}
