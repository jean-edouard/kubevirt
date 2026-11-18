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

package main

import (
	"slices"
	"testing"
)

// The same digests as a WORKSPACE writes them, in hex, and as an
// rpms.json integrity field writes them, in base64
const (
	hexMirrored    = "dd11bab2ea0abdfa310362eace871422a003340bf223135626500f8f5a985f6b"
	base64Mirrored = "3RG6suoKvfoxA2LqzocUIqADNAvyIxNWJlAPj1qYX2s="

	hexUnmirrored    = "153742d20ad378247d8256619d2fca5666565befd8dbf0743966da331c993540"
	base64Unmirrored = "FTdC0grTeCR9glZhnS/KVmZWW+/Y2/B0OWbaMxyZNUA="

	hexArchive = "9e6aa0c60204bb4b152ce541ca3a9f5c28b020ed551dd417d3936a8b2153f0df"
)

// testWorkspace is a WORKSPACE as a release branch carries it: one
// mirrored rpm rule, one that is only reachable upstream, and a non rpm
// rule that is mirrored elsewhere
const testWorkspace = `
rpm(
    name = "acl-0__2.3.1-4.el9.x86_64",
    sha256 = "` + hexMirrored + `",
    urls = [
        "http://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/Packages/acl-2.3.1-4.el9.x86_64.rpm",
        "` + builddepsPrefix + hexMirrored + `",
    ],
)

rpm(
    name = "attr-0__2.5.1-3.el9.x86_64",
    sha256 = "` + hexUnmirrored + `",
    urls = [
        "http://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/Packages/attr-2.5.1-3.el9.x86_64.rpm",
    ],
)

http_archive(
    name = "libguestfs-appliance-x86_64",
    sha256 = "` + hexArchive + `",
    urls = ["https://storage.googleapis.com/kubevirt-prow/devel/libguestfs-appliance.tar.xz"],
)
`

// testRPMsJSON is an rpms.json as main carries it, declaring the same
// two rpms as testWorkspace
const testRPMsJSON = `{
	"name": "kubevirt-rpms",
	"rpms": [
		{
			"name": "acl-0__2.3.1-4.el9.x86_64",
			"integrity": "sha256-` + base64Mirrored + `",
			"urls": [
				"http://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/Packages/acl-2.3.1-4.el9.x86_64.rpm",
				"` + builddepsPrefix + hexMirrored + `"
			]
		},
		{
			"name": "attr-0__2.5.1-3.el9.x86_64",
			"integrity": "sha256-` + base64Unmirrored + `",
			"urls": [
				"http://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/Packages/attr-2.5.1-3.el9.x86_64.rpm"
			]
		}
	]
}`

func digestsOf(t *testing.T, content string) []digest {
	t.Helper()
	digests, err := declaredDigests(content)
	if err != nil {
		t.Fatalf("failed to collect the digests: %v", err)
	}
	return digests
}

func hexesOf(digests []digest) []string {
	var hexes []string
	for _, d := range digests {
		hexes = append(hexes, d.hex)
	}
	return hexes
}

func TestDeclaredDigestsOfAWorkspace(t *testing.T) {
	digests := digestsOf(t, testWorkspace)

	expected := []digest{
		{hex: hexMirrored, line: 4},
		{hex: hexUnmirrored, line: 13},
		{hex: hexArchive, line: 21},
	}
	if !slices.Equal(digests, expected) {
		t.Errorf("expected %v, got %v", expected, digests)
	}
}

func TestDeclaredDigestsOfAnRPMsJSON(t *testing.T) {
	digests := digestsOf(t, testRPMsJSON)

	// the base64 integrity fields have to come back as the hex the
	// builddeps bucket serves them under
	expected := []digest{
		{hex: hexMirrored, line: 6},
		{hex: hexUnmirrored, line: 14},
	}
	if !slices.Equal(digests, expected) {
		t.Errorf("expected %v, got %v", expected, digests)
	}
}

// The checker reads a declared digest wherever it sits, so reformatting
// the dependency file can never turn it into a silent no-op
func TestDeclaredDigestsIgnoreFormatting(t *testing.T) {
	for name, content := range map[string]string{
		"indented":   `    rpm(name = "acl", sha256 = "` + hexMirrored + `")`,
		"one line":   `rpm(name="acl",sha256="` + hexMirrored + `",urls=["x"])`,
		"renamed":    `some_future_rule(sha256 = "` + hexMirrored + `")`,
		"no padding": `sha256="` + hexMirrored + `"`,
		"minified":   `{"integrity":"sha256-` + base64Mirrored + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if hexes := hexesOf(digestsOf(t, content)); !slices.Equal(hexes, []string{hexMirrored}) {
				t.Errorf("expected [%s], got %v", hexMirrored, hexes)
			}
		})
	}
}

func TestDeclaredDigestsOfAFileWithout(t *testing.T) {
	// the caller turns this into a hard failure, since a dependency file
	// that declares nothing means the format moved
	if digests := digestsOf(t, "nothing to see here\n"); len(digests) != 0 {
		t.Errorf("expected no digest, got %v", digests)
	}
}

func TestUnmirrored(t *testing.T) {
	for name, content := range map[string]string{
		"WORKSPACE": testWorkspace,
		"rpms.json": testRPMsJSON,
	} {
		t.Run(name, func(t *testing.T) {
			missing := unmirrored(content, digestsOf(t, content), nil)
			if !slices.Equal(hexesOf(missing), expectedMissing(name)) {
				t.Errorf("expected %v, got %v", expectedMissing(name), hexesOf(missing))
			}
		})
	}
}

// the WORKSPACE also carries an archive that no builddeps url covers
func expectedMissing(file string) []string {
	if file == "WORKSPACE" {
		return []string{hexUnmirrored, hexArchive}
	}
	return []string{hexUnmirrored}
}

func TestUnmirroredSkipsExemptDigests(t *testing.T) {
	exempt := exemptDigests(hexArchive + " , " + hexUnmirrored)
	missing := unmirrored(testWorkspace, digestsOf(t, testWorkspace), exempt)
	if len(missing) != 0 {
		t.Errorf("expected the exempt digests to be skipped, got %v", hexesOf(missing))
	}
}

func TestUnmirroredReportsEveryDigestOnce(t *testing.T) {
	// the same rpm is declared for several architectures
	content := `
rpm(sha256 = "` + hexUnmirrored + `")
rpm(sha256 = "` + hexUnmirrored + `")
`
	missing := unmirrored(content, digestsOf(t, content), nil)
	if !slices.Equal(hexesOf(missing), []string{hexUnmirrored}) {
		t.Errorf("expected a single report, got %v", hexesOf(missing))
	}
}

func TestExemptDigests(t *testing.T) {
	if exempt := exemptDigests(""); len(exempt) != 0 {
		t.Errorf("expected no exempt digest, got %v", exempt)
	}
	if exempt := exemptDigests(" " + hexArchive + ", ,"); len(exempt) != 1 || !exempt[hexArchive] {
		t.Errorf("expected only %s to be exempt, got %v", hexArchive, exempt)
	}
}

func TestDecodeBase64Digest(t *testing.T) {
	if sum, err := decodeBase64Digest(base64Mirrored); err != nil || sum != hexMirrored {
		t.Errorf("expected %s, got %q (%v)", hexMirrored, sum, err)
	}
	if _, err := decodeBase64Digest("not base64!!"); err == nil {
		t.Error("expected an error for a malformed base64 sha256")
	}
	if _, err := decodeBase64Digest("c2VjcmV0"); err == nil {
		t.Error("expected an error for a sha256 that is not 32 bytes")
	}
}
