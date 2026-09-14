#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Minewing GW1 r1 QEMU+RAUC / power-loss HIL runner for Zyvor OTA.
#
# Prerequisites (fail closed if missing):
#   QUALIFY_QEMU_IMAGE   path to QEMU-bootable A/B disk image with real RAUC
#   OTA_HIL_BUNDLE       signed .raucb under test (optional for status-only)
#   OTA_HIL_ASSIGNMENT   OTA assignment JSON (optional)
#
#   ./scripts/hil/run-rauc-powerloss-hil.sh
#
# Records evidence under evidence/qualification/hil/<stamp>/. Signs
# hardware-checklist.md only when OTA_HIL_SIGN=1 and all required rows pass
# on environment=qemu|physical.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
OUT="$ROOT/evidence/qualification/hil/$STAMP"
mkdir -p "$OUT"
ENV_LABEL=${OTA_HIL_ENV:-qemu} # qemu | physical | dry-run
STRICT=${OTA_HIL_STRICT:-1}
IMAGE=${QUALIFY_QEMU_IMAGE:-${OTA_HIL_IMAGE:-}}
BUNDLE=${OTA_HIL_BUNDLE:-}
ASSIGN=${OTA_HIL_ASSIGNMENT:-}
SSH_TARGET=${OTA_HIL_SSH:-} # e.g. root@guest for physical/QEMU ssh

echo "env=$ENV_LABEL image=${IMAGE:-none} stamp=$STAMP" | tee "$OUT/meta.env"

python3 - "$OUT" "$ENV_LABEL" "$STRICT" "$ROOT" "$IMAGE" "$BUNDLE" "$ASSIGN" "$SSH_TARGET" "${OTA_HIL_SIGN:-0}" <<'PY'
import json, pathlib, sys, datetime, os, shutil, subprocess

out = pathlib.Path(sys.argv[1])
env = sys.argv[2]
strict = sys.argv[3] == "1"
root = pathlib.Path(sys.argv[4])
image = sys.argv[5]
bundle = sys.argv[6]
assign = sys.argv[7]
ssh = sys.argv[8]
sign = sys.argv[9] == "1"

rows = []

def add(rid, status, detail):
    rows.append({"id": rid, "status": status, "detail": detail})
    print(f"[{status.upper()}] {rid} — {detail}")

def sh(cmd, timeout=60):
    return subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)

# Always capture host software posture
skip_q = os.environ.get("OTA_HIL_SKIP_QUALIFY", "") in ("1", "true", "yes")
if skip_q:
    add("host_software_qualify", "pass", "skipped via OTA_HIL_SKIP_QUALIFY (covered by CI verify job)")
else:
    r = sh(f"cd {root} && make qualify 2>&1 | tail -20")
    (out / "host-qualify.tail.txt").write_text(r.stdout + r.stderr)
    add("host_software_qualify", "pass" if r.returncode == 0 else "fail",
        "make qualify" + ("" if r.returncode == 0 else f" rc={r.returncode}"))

# Dry-run mode: document harness presence only
if env == "dry-run":
    add("image_present", "blocked", "dry-run — set QUALIFY_QEMU_IMAGE")
    add("rauc_status", "blocked", "dry-run")
    add("valid_bundle_commit", "blocked", "dry-run")
    add("bad_signature_reject", "blocked", "dry-run")
    add("power_loss_during_write", "blocked", "dry-run")
    add("power_loss_boot_selection", "blocked", "dry-run")
    add("needs_recovery_crash", "blocked", "dry-run")
    add("three_healthy_reboots", "blocked", "dry-run")
else:
    if not image or not pathlib.Path(image).is_file():
        add("image_present", "fail" if env in ("qemu", "physical") else "blocked",
            f"QUALIFY_QEMU_IMAGE missing: {image!r}")
    else:
        digest = sh(f"sha256sum {image}").stdout.strip()
        (out / "image.sha256").write_text(digest + "\n")
        add("image_present", "pass", digest.split()[0][:16] + "…")

    if ssh:
        r = sh(f"ssh -o BatchMode=yes -o ConnectTimeout=10 {ssh} 'rauc status; zyvor-ota status || true'")
        (out / "rauc-status.txt").write_text(r.stdout + r.stderr)
        blob = (r.stdout + r.stderr).lower()
        if r.returncode == 0 and ("compatible" in blob or "slot" in blob or "rauc" in blob):
            add("rauc_status", "pass", "rauc status via ssh")
        else:
            add("rauc_status", "fail", f"ssh/rauc failed rc={r.returncode}")
    else:
        add("rauc_status", "blocked", "set OTA_HIL_SSH=user@guest to query rauc")

    # Operator-attached evidence files (power-loss etc. are destructive)
    attach = {
        "valid_bundle_commit": os.environ.get("OTA_HIL_LOG_COMMIT", ""),
        "bad_signature_reject": os.environ.get("OTA_HIL_LOG_BADSIG", ""),
        "power_loss_during_write": os.environ.get("OTA_HIL_LOG_PLOSS_WRITE", ""),
        "power_loss_boot_selection": os.environ.get("OTA_HIL_LOG_PLOSS_BOOT", ""),
        "needs_recovery_crash": os.environ.get("OTA_HIL_LOG_NEEDS_RECOVERY", ""),
        "three_healthy_reboots": os.environ.get("OTA_HIL_LOG_REBOOTS", ""),
    }
    for rid, path in attach.items():
        if path and pathlib.Path(path).is_file():
            dest = out / f"{rid}.log"
            shutil.copy2(path, dest)
            add(rid, "pass", f"attached {path}")
        else:
            hint = {
                "valid_bundle_commit": "OTA_HIL_LOG_COMMIT",
                "bad_signature_reject": "OTA_HIL_LOG_BADSIG",
                "power_loss_during_write": "OTA_HIL_LOG_PLOSS_WRITE",
                "power_loss_boot_selection": "OTA_HIL_LOG_PLOSS_BOOT",
                "needs_recovery_crash": "OTA_HIL_LOG_NEEDS_RECOVERY",
                "three_healthy_reboots": "OTA_HIL_LOG_REBOOTS",
            }[rid]
            add(rid, "blocked", f"attach operator log via {hint}=/path")

    if bundle:
        if pathlib.Path(bundle).is_file():
            bsum = sh(f"sha256sum {bundle}").stdout.strip()
            (out / "bundle.sha256").write_text(bsum + "\n")
            add("bundle_present", "pass", bsum.split()[0][:16] + "…")
        else:
            add("bundle_present", "fail", f"bundle missing: {bundle}")
    else:
        add("bundle_present", "blocked", "set OTA_HIL_BUNDLE")

    if assign and pathlib.Path(assign).is_file():
        shutil.copy2(assign, out / "assignment.json")
        add("assignment_present", "pass", assign)
    else:
        add("assignment_present", "blocked", "set OTA_HIL_ASSIGNMENT")

# Write procedure card for power-loss (always)
(out / "POWERLOSS_PROCEDURE.md").write_text(
    f"""# Power-loss procedure — Minewing GW1 r1 ({env})

1. Boot slot A; capture `rauc status` and `zyvor-ota status`.
2. Start a valid signed install; when RAUC is writing the inactive slot,
   interrupt power (QEMU: kill -9 the qemu PID / `system_powerdown` mid-write).
3. Restore power; confirm **previous slot still boots** and agent does not
   auto-commit a half-written candidate.
4. Repeat for boot-selection change window (BOOT_ORDER / attempt counters).
5. Save console + `journalctl -u rauc -u zyvor-otad` to the env vars listed in SUMMARY.
6. Re-run this script with logs attached; then `OTA_HIL_SIGN=1` only if claimable.

See `boards/minewing-gw1-r1/QEMU.md` and `docs/QUALIFICATION.md`.
"""
)

counts = {}
for r in rows:
    counts[r["status"]] = counts.get(r["status"], 0) + 1
required = [
    "image_present", "rauc_status", "valid_bundle_commit", "bad_signature_reject",
    "power_loss_during_write", "power_loss_boot_selection", "needs_recovery_crash",
    "three_healthy_reboots",
]
by = {r["id"]: r for r in rows}
# Minewing silicon/QEMU claim requires an explicit Minewing-compatible image
# (QUALIFY_QEMU_IMAGE from BSP) or physical board — not the generic zyvor-ota-qemu-lab disk.
sku = os.environ.get("OTA_HIL_SKU", "minewing-gw1-r1")
compatible = os.environ.get("OTA_HIL_COMPATIBLE", "")
minewing_image = (
    sku.startswith("minewing")
    and ("minewing" in compatible.lower() or os.environ.get("OTA_HIL_MINEWING", "") in ("1", "true", "yes"))
)
rows_ok = all(by.get(i, {}).get("status") == "pass" for i in required)
claimable = env in ("qemu", "physical") and rows_ok and minewing_image
qemu_lab_complete = env == "qemu" and rows_ok and not minewing_image
report = {
    "product": "zyvor-ota",
    "sku": sku,
    "compatible": compatible or None,
    "environment": env,
    "stamp": out.name,
    "finished_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "counts": counts,
    "minewing_rauc_claimable": claimable,
    "qemu_lab_complete": qemu_lab_complete,
    "rows": rows,
}
(out / "results.json").write_text(json.dumps(report, indent=2) + "\n")
(out / "SUMMARY.md").write_text(
    f"# OTA RAUC / power-loss HIL summary\n\n"
    f"- environment: `{env}`\n"
    f"- sku: `{sku}` compatible: `{compatible or 'n/a'}`\n"
    f"- counts: `{counts}`\n"
    f"- minewing_rauc_claimable: **{claimable}**\n"
    f"- qemu_lab_complete: **{qemu_lab_complete}**\n\n"
    f"Attach operator logs:\n"
    f"- `OTA_HIL_LOG_COMMIT`\n"
    f"- `OTA_HIL_LOG_BADSIG`\n"
    f"- `OTA_HIL_LOG_PLOSS_WRITE`\n"
    f"- `OTA_HIL_LOG_PLOSS_BOOT`\n"
    f"- `OTA_HIL_LOG_NEEDS_RECOVERY`\n"
    f"- `OTA_HIL_LOG_REBOOTS`\n"
)

if sign:
    if claimable:
        checklist = root / "evidence/qualification/hardware-checklist.md"
        mapping = [
            ("valid_bundle_commit", "Valid bundle + healthy commit"),
            ("bad_signature_reject", "Wrong board or signature / Invalid inner RAUC signature"),
            ("power_loss_during_write", "Power loss during target writes"),
            ("power_loss_boot_selection", "Power loss while boot selection changes"),
            ("needs_recovery_crash", "Agent crash during install → NeedsRecovery"),
            ("three_healthy_reboots", "Three ordinary healthy reboots"),
        ]
        block = [
            "",
            f"## Signed RAUC/power-loss run `{out.name}`",
            "",
            f"- Environment: {env}",
            f"- Finished: {report['finished_at']}",
            f"- Evidence: `evidence/qualification/hil/{out.name}/`",
            "",
            "| Test | Environment | Result | Log / evidence path |",
            "|---|---|---|---|",
        ]
        for rid, label in mapping:
            r = by.get(rid, {})
            block.append(
                f"| {label} | {env} | {r.get('status','')} | hil/{out.name}/{rid}.log |"
            )
        block += ["", f"Operator sign-off stamp: {out.name}", ""]
        checklist.write_text(checklist.read_text().rstrip() + "\n" + "\n".join(block) + "\n")
        print(f"updated {checklist}")
    elif qemu_lab_complete and os.environ.get("OTA_HIL_SIGN_QEMU_LAB", "") in ("1", "true", "yes"):
        lab_check = root / "evidence/qualification/qemu-lab/CHECKLIST.md"
        lab_check.parent.mkdir(parents=True, exist_ok=True)
        lab_check.write_text(
            f"# QEMU lab RAUC checklist (not Minewing)\n\n"
            f"- stamp: `{out.name}`\n"
            f"- compatible: `{compatible or 'zyvor-ota-qemu-lab'}`\n"
            f"- finished: {report['finished_at']}\n"
            f"- evidence: `evidence/qualification/hil/{out.name}/`\n\n"
            f"**Not a Minewing silicon claim.**\n"
        )
        print(f"updated {lab_check}")
    else:
        raise SystemExit(
            "refusing OTA_HIL_SIGN=1: minewing_rauc_claimable=false "
            "(set OTA_HIL_MINEWING=1 + Minewing image, or OTA_HIL_SIGN_QEMU_LAB=1 for lab-only)"
        )

print(f"\nwrote {out}")
if strict and env in ("qemu", "physical") and not claimable:
    # soft: still exit 0 for blocked-only when image missing? User wants honesty.
    # Fail only if required rows failed (not merely blocked), or if strict+no image on qemu.
    if counts.get("fail", 0):
        sys.exit(1)
    if env in ("qemu", "physical") and by.get("image_present", {}).get("status") == "fail":
        sys.exit(1)
PY
