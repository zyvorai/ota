#!/usr/bin/env python3
"""Source-distribution SPDX 2.3 SBOM. Does not claim to inventory the board OS.

Inventories source files and the one vendored dependency. The source tree digest
identifies this exact input; CI produces a fresh SBOM for each tagged build.
"""
import hashlib
import json
import pathlib
from datetime import datetime, timezone

root = pathlib.Path(__file__).resolve().parents[1]
files = []
relationships = []
for path in sorted(root.rglob("*")):
    rel = path.relative_to(root)
    if not path.is_file() or any(p in {".git", "bin", "dist", "__pycache__", "evidence"} for p in rel.parts):
        continue
    if path.name in {"sbom.spdx.json", "coverage.out", "TEST-REPORT.md"}:
        continue
    file_id = "SPDXRef-File-" + hashlib.sha256(str(rel).encode()).hexdigest()[:24]
    checksum = hashlib.sha256(path.read_bytes()).hexdigest()
    files.append({"SPDXID": file_id, "fileName": "./" + str(rel),
                  "checksums": [{"algorithm": "SHA256", "checksumValue": checksum}],
                  "licenseConcluded": "NOASSERTION", "copyrightText": "NOASSERTION"})
    package = "SPDXRef-Godbus" if rel.parts[0] == "vendor" else "SPDXRef-ZyvorOTA"
    relationships.append({"spdxElementId": package, "relationshipType": "CONTAINS", "relatedSpdxElement": file_id})
tree = hashlib.sha256(json.dumps(files, sort_keys=True).encode()).hexdigest()
sbom = {"spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT",
        "name": "zyvor-ota-source-0.1.0", "documentNamespace": f"https://zyvor.dev/spdx/ota/{tree}",
        "creationInfo": {"creators": ["Tool: zyvor-ota-source-sbom-0.1.0"],
                         "created": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")},
        "packages": [
            {"SPDXID": "SPDXRef-ZyvorOTA", "name": "zyvor-ota", "versionInfo": "0.1.0",
             "downloadLocation": "NOASSERTION", "filesAnalyzed": False, "licenseDeclared": "Apache-2.0"},
            {"SPDXID": "SPDXRef-Godbus", "name": "github.com/godbus/dbus/v5", "versionInfo": "v5.1.0",
             "downloadLocation": "https://github.com/godbus/dbus/tree/v5.1.0",
             "filesAnalyzed": False, "licenseDeclared": "BSD-2-Clause",
             "externalRefs": [{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl",
                               "referenceLocator": "pkg:golang/github.com/godbus/dbus/v5@v5.1.0"}]}],
        "files": files, "relationships": relationships + [
            {"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": "SPDXRef-ZyvorOTA"},
            {"spdxElementId": "SPDXRef-ZyvorOTA", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": "SPDXRef-Godbus"}]}
(root / "sbom.spdx.json").write_text(json.dumps(sbom, indent=2) + "\n")
print(f"Source SBOM: {len(files)} files, 2 packages")
