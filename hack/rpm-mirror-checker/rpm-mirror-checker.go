/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright The KubeVirt Authors.
 *
 */

// The mirror checker verifies that every artifact kubevirt builds against
// is served by the builddeps bucket, the only mirror the project owns.
// Upstream mirrors drop old packages, so an artifact that is only
// reachable upstream breaks the build of a release branch as soon as it
// ages out.
//
// The builddeps bucket is content addressed: it serves every artifact
// under its own sha256. The mirror url of an artifact is therefore a pure
// function of the digest its dependency file already declares, and
// checking mirror coverage needs no understanding of the file's
// structure. For every declared digest, the matching builddeps url has to
// appear somewhere in the same file.
//
// That makes one checker enough for every branch. main declares its
// artifacts in the rpms.json lockfile of bazel 9, release branches
// declare theirs as Starlark rules in WORKSPACE, and neither format has
// to be parsed to answer the question.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// builddepsPrefix is the kubevirt builddeps bucket, the only mirror owned
// by the project. It serves every artifact under its hex sha256.
const builddepsPrefix = "https://storage.googleapis.com/builddeps/"

// dependencyFiles are the files that declare mirrored artifacts, in the
// order they are looked for. main carries rpms.json, release branches
// still carry WORKSPACE.
var dependencyFiles = []string{"rpms.json", "WORKSPACE"}

// digestFormat matches a sha256 as one dependency file format declares
// it. Capturing group 1 is the digest, which decode normalizes to hex.
type digestFormat struct {
	pattern *regexp.Regexp
	decode  func(string) (string, error)
}

var digestFormats = []digestFormat{
	{
		// WORKSPACE: sha256 = "dd11bab2ea0abdfa310362eace871422..."
		pattern: regexp.MustCompile(`\bsha256\s*=\s*"([0-9a-f]{64})"`),
		decode:  func(digest string) (string, error) { return digest, nil },
	},
	{
		// rpms.json: "integrity": "sha256-3RG6suoKvfoxA2LqzocUIqADNAvy..."
		pattern: regexp.MustCompile(`"integrity"\s*:\s*"sha256-([A-Za-z0-9+/]{43}=)"`),
		decode:  decodeBase64Digest,
	},
}

// decodeBase64Digest turns the base64 sha256 of an rpms.json integrity
// field into the hex form the builddeps bucket serves it under
func decodeBase64Digest(digest string) (string, error) {
	sum, err := base64.StdEncoding.DecodeString(digest)
	if err != nil {
		return "", fmt.Errorf("malformed base64 sha256 %q: %w", digest, err)
	}
	if len(sum) != sha256.Size {
		return "", fmt.Errorf("sha256 %q decodes to %d bytes, expected %d", digest, len(sum), sha256.Size)
	}
	return hex.EncodeToString(sum), nil
}

type options struct {
	path   string
	exempt string
}

func flagOptions() options {
	o := options{}
	flag.StringVar(&o.path, "file", "", "The dependency file to check, by default the first of "+strings.Join(dependencyFiles, " or ")+" that exists")
	flag.StringVar(&o.exempt, "exempt", "", "Comma separated hex sha256 digests that are allowed to have no builddeps mirror")
	flag.Parse()
	return o
}

// digest is a sha256 declared by a dependency file, in hex, along with
// the line that declares it
type digest struct {
	hex  string
	line int
}

// lineIndex holds the offset of every newline of a file, so that the line
// of a match can be looked up without rescanning the file every time
type lineIndex []int

func newLineIndex(content string) lineIndex {
	var index lineIndex
	for offset, b := range []byte(content) {
		if b == '\n' {
			index = append(index, offset)
		}
	}
	return index
}

// line returns the 1 based line that contains offset
func (l lineIndex) line(offset int) int {
	return sort.SearchInts(l, offset) + 1
}

// declaredDigests returns every sha256 a dependency file declares, in hex
func declaredDigests(content string) ([]digest, error) {
	index := newLineIndex(content)
	var digests []digest
	for _, format := range digestFormats {
		for _, match := range format.pattern.FindAllStringSubmatchIndex(content, -1) {
			line := index.line(match[0])
			sum, err := format.decode(content[match[2]:match[3]])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			digests = append(digests, digest{hex: sum, line: line})
		}
	}
	sort.SliceStable(digests, func(i, j int) bool { return digests[i].line < digests[j].line })
	return digests, nil
}

// unmirrored returns the declared digests that the builddeps bucket does
// not serve, at most once per digest
func unmirrored(content string, digests []digest, exempt map[string]bool) []digest {
	var missing []digest
	reported := map[string]bool{}
	for _, d := range digests {
		if exempt[d.hex] || reported[d.hex] {
			continue
		}
		reported[d.hex] = true
		if strings.Contains(content, builddepsPrefix+d.hex) {
			continue
		}
		missing = append(missing, d)
	}
	return missing
}

// exemptDigests parses the comma separated digests of the -exempt flag
func exemptDigests(flagValue string) map[string]bool {
	exempt := map[string]bool{}
	for _, d := range strings.Split(flagValue, ",") {
		if d = strings.TrimSpace(d); d != "" {
			exempt[d] = true
		}
	}
	return exempt
}

// dependencyFile returns the dependency file of the current branch
func dependencyFile() (string, error) {
	for _, path := range dependencyFiles {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("found none of %s in the working directory, use -file", strings.Join(dependencyFiles, ", "))
}

func main() {
	// Under bazel run the binary starts in its runfiles tree, so move to
	// the workspace to let the dependency file be found by its name
	if path := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); path != "" {
		if err := os.Chdir(path); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to enter %s: %v\n", path, err)
			os.Exit(1)
		}
	}

	opts := flagOptions()

	path := opts.path
	if path == "" {
		var err error
		if path, err = dependencyFile(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to read %s: %v\n", path, err)
		os.Exit(1)
	}
	content := string(data)

	digests, err := declaredDigests(content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", path, err)
		os.Exit(1)
	}

	// A dependency file always declares digests. Finding none means the
	// format changed under the checker, which has to fail loudly instead
	// of reporting that nothing is wrong.
	if len(digests) == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: no sha256 found in %s, the format it is declared in must have changed\n", path)
		os.Exit(1)
	}

	missing := unmirrored(content, digests, exemptDigests(opts.exempt))
	for _, d := range missing {
		fmt.Printf("  %s:%d: %s is not mirrored (add %s%s)\n", path, d.line, d.hex, builddepsPrefix, d.hex)
	}
	fmt.Printf("Checked %d sha256 in %s\n", len(digests), path)

	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "FAILURE: %d artifacts of %s are not served by the builddeps bucket\n", len(missing), path)
		os.Exit(1)
	}
	fmt.Printf("SUCCESS: every artifact of %s is served by the builddeps bucket\n", path)
}
