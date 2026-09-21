// SPDX-License-Identifier: Apache-2.0
package ota

import (
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestReleaseSchemaAcceptsSchema1AndSchema2(t *testing.T) {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	sch, err := c.Compile("../../api/release.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	valid := []string{
		`{
			"schema": 1, "id": "tutorial-release-1", "sequence": 1,
			"compatible": "tutorial-board", "backend": "simulator", "version": "1.0.0",
			"expires": "2030-01-01T00:00:00Z",
			"artifact": {"url": "http://127.0.0.1:8091/os.raucb", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size": 24}
		}`,
		`{
			"schema": 1, "id": "tutorial-release-sbom", "sequence": 1,
			"compatible": "tutorial-board", "backend": "simulator", "version": "1.0.0",
			"expires": "2030-01-01T00:00:00Z",
			"sbom_sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			"sbom": {"url": "https://downloads.example/image.spdx.json", "sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "size": 12},
			"artifact": {"url": "http://127.0.0.1:8091/os.raucb", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size": 24}
		}`,
		`{
			"schema": 2, "id": "rel-2", "sequence": 2,
			"compatible": "tutorial-board", "backend": "simulator", "version": "2026.10.0",
			"expires": "2030-01-01T00:00:00Z",
			"targets": [
				{"id": "os", "type": "os.rauc", "reboot": "required", "rollback": "slot",
				 "artifact": {"url": "https://downloads.example/os.raucb", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "size": 8}},
				{"id": "cfg", "type": "config.bundle", "reboot": "none", "rollback": "previous-digest",
				 "requires": "gpu.nvidia", "depends_on": ["os"],
				 "artifact": {"url": "https://downloads.example/cfg.bin", "sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "size": 4}}
			]
		}`,
	}
	for _, doc := range valid {
		if err := validateRelease(sch, doc); err != nil {
			t.Fatal(err)
		}
	}
	invalid := []string{
		`{"schema": 1, "id": "r", "sequence": 1, "compatible": "b", "backend": "simulator", "version": "1", "expires": "2030-01-01T00:00:00Z", "artifact": {"url": "https://downloads.example/a", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size": 1}, "targets": [{"id": "os", "type": "os.rauc", "reboot": "required", "artifact": {"url": "https://downloads.example/a", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size": 1}}]}`,
		`{"schema": 2, "id": "r", "sequence": 1, "compatible": "b", "backend": "simulator", "version": "1", "expires": "2030-01-01T00:00:00Z", "targets": []}`,
		`{"schema": 2, "id": "r", "sequence": 1, "compatible": "b", "backend": "simulator", "version": "1", "expires": "2030-01-01T00:00:00Z", "targets": [{"id": "os", "type": "os.rauc", "artifact": {"url": "https://downloads.example/a", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size": 1}}]}`,
		`{"schema": 2, "id": "r", "sequence": 1, "compatible": "b", "backend": "simulator", "version": "1", "expires": "2030-01-01T00:00:00Z", "targets": [{"id": "cfg", "type": "config.bundle", "reboot": "required", "artifact": {"url": "https://downloads.example/a", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size": 1}}]}`,
		`{"schema": 3, "id": "r", "sequence": 1, "compatible": "b", "backend": "simulator", "version": "1", "expires": "2030-01-01T00:00:00Z", "artifact": {"url": "https://downloads.example/a", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size": 1}}`,
		`{"schema": 1, "id": "r", "sequence": 1, "compatible": "b", "backend": "simulator", "version": "1", "expires": "2030-01-01T00:00:00Z", "sbom_sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "artifact": {"url": "https://downloads.example/a", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size": 1}}`,
	}
	for _, doc := range invalid {
		if err := validateRelease(sch, doc); err == nil {
			t.Fatalf("accepted invalid release: %s", doc)
		}
	}
}

func validateRelease(sch *jsonschema.Schema, doc string) error {
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
	if err != nil {
		return err
	}
	return sch.Validate(inst)
}
