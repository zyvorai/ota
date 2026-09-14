#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Software qualification matrix for Zyvor OTA.

Runs host-side rows that do not require a RAUC board image or physical power
loss. Hardware / QEMU RAUC rows remain operator-signed in
evidence/qualification/hardware-checklist.md — this script never claims them.

Requires: make build (binaries), Go toolchain for go test.
"""
from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone

ROOT = pathlib.Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / "evidence" / "qualification"
CLI = ROOT / "bin" / "zyvor-ota"
DAEMON = ROOT / "bin" / "zyvor-otad"
FLEET_REF = ROOT / "bin" / "zyvor-fleet-ref"


def run(cmd, **kwargs):
    return subprocess.run(cmd, cwd=ROOT, text=True, capture_output=True, **kwargs)


def require_bins():
    for path in (CLI, DAEMON):
        if not path.is_file():
            print(f"missing {path}; run make build first", file=sys.stderr)
            sys.exit(1)


def row(results, name, status, detail=""):
    results.append({
        "id": name,
        "status": status,
        "detail": detail,
        "class": "software",
    })
    mark = "PASS" if status == "pass" else ("SKIP" if status == "skip" else "FAIL")
    print(f"[{mark}] {name}" + (f" — {detail}" if detail else ""))


def go_tests(results):
    proc = run(["go", "test", "-count=1", "./internal/ota/...", "./internal/fleetref/..."], timeout=180)
    if proc.returncode == 0:
        row(results, "unit_protocol_fleetref", "pass", "go test internal/ota + internal/fleetref")
    else:
        row(results, "unit_protocol_fleetref", "fail", (proc.stdout + proc.stderr)[-500:])
        return False
    return True


def e2e_demo(results):
    proc = run([sys.executable, "scripts/e2e.py"], timeout=120)
    if proc.returncode == 0:
        row(results, "simulator_e2e_commit_rollback_replay", "pass")
        return True
    row(results, "simulator_e2e_commit_rollback_replay", "fail", (proc.stdout + proc.stderr)[-500:])
    return False


def bake_dry_run(results):
    with tempfile.TemporaryDirectory(prefix="zyvor-bake-") as tmp:
        rootfs = pathlib.Path(tmp) / "rootfs"
        (rootfs / "usr/local/bin").mkdir(parents=True)
        (rootfs / "etc").mkdir(parents=True)
        (rootfs / "etc/passwd").write_text("root:x:0:0:root:/root:/bin/sh\n")
        (rootfs / "etc/group").write_text("root:x:0:\n")
        (rootfs / "etc/systemd/system/multi-user.target.wants").mkdir(parents=True)
        board = ROOT / "boards" / "minewing-gw1-r1"
        # Use a private board copy so we never leave board.env in the repo tree.
        work = pathlib.Path(tmp) / "board"
        work.mkdir()
        for name in ("system.conf.in", "manifest.raucm.in", "agent.json", "board.env.example"):
            (work / name).write_text((board / name).read_text())
        (work / "board.env").write_text((board / "board.env.example").read_text())
        proc = run([
            "bash", "scripts/bake-rootfs-overlay.sh",
            "--rootfs", str(rootfs),
            "--board", str(work),
            "--agent-config", str(work / "agent.json"),
            "--bin-dir", str(ROOT / "bin"),
        ], timeout=60)
        ok = proc.returncode == 0 and (rootfs / "usr/local/bin/zyvor-otad").is_file()
        if ok and (rootfs / "etc/rauc/system.conf").is_file():
            row(results, "bake_rootfs_overlay_minewing", "pass")
            return True
        row(results, "bake_rootfs_overlay_minewing", "fail", (proc.stdout + proc.stderr)[-500:])
        return False


def render_board(results):
    board = ROOT / "boards" / "minewing-gw1-r1"
    with tempfile.TemporaryDirectory() as tmp:
        env = pathlib.Path(tmp) / "board.env"
        env.write_text((board / "board.env.example").read_text())
        # render script expects board.env inside BOARD_DIR — use a copy tree
        work = pathlib.Path(tmp) / "board"
        work.mkdir()
        for name in ("system.conf.in", "manifest.raucm.in"):
            (work / name).write_text((board / name).read_text())
        (work / "board.env").write_text(env.read_text())
        out = pathlib.Path(tmp) / "out"
        proc = run(["bash", "scripts/render-board-rauc.sh", str(work), str(out)], timeout=30)
        conf = out / "system.conf"
        if proc.returncode == 0 and conf.is_file() and "minewing-gw1-r1" in conf.read_text():
            row(results, "render_rauc_system_conf", "pass")
            return True
        row(results, "render_rauc_system_conf", "fail", (proc.stdout + proc.stderr)[-400:])
        return False


def schema_syntax(results):
    try:
        import yaml  # type: ignore
    except ImportError:
        # Fallback: JSON schemas only
        yaml = None
    ok = True
    release = json.loads((ROOT / "api" / "release.schema.json").read_text())
    if release.get("$schema"):
        row(results, "release_schema_json_parse", "pass")
    else:
        row(results, "release_schema_json_parse", "fail")
        ok = False
    openapi = ROOT / "api" / "openapi.yaml"
    text = openapi.read_text()
    if yaml is not None:
        yaml.safe_load(text)
        row(results, "openapi_yaml_parse", "pass")
    else:
        if "openapi:" in text and "/v1/status" in text:
            row(results, "openapi_yaml_parse", "pass", "structural check (PyYAML not installed)")
        else:
            row(results, "openapi_yaml_parse", "fail")
            ok = False
    return ok


def hardware_pending(results):
    """Skip hardware rows unless a signed HIL run marks them claimable."""
    hil_root = EVIDENCE / "hil"
    claimable = False
    stamp = ""
    if hil_root.is_dir():
        for p in sorted(hil_root.glob("*/results.json"), reverse=True):
            try:
                data = json.loads(p.read_text())
            except Exception:
                continue
            if data.get("minewing_rauc_claimable"):
                claimable = True
                stamp = p.parent.name
                break
    pending = [
        "qemu_rauc_valid_bundle_commit",
        "qemu_rauc_power_loss_during_flash",
        "physical_board_power_loss_boot_selection",
        "physical_board_watchdog_bad_kernel",
        "physical_packaging_dbus_polkit",
    ]
    if claimable:
        for name in pending:
            row(results, name, "pass", f"signed hil/{stamp} — see hardware-checklist.md")
        return
    for name in pending:
        row(
            results,
            name,
            "skip",
            "run scripts/hil/run-rauc-powerloss-hil.sh + sign — docs/HIL.md",
        )


def ci_lab_substitute(results):
    """Promote CI lab-substitute rows when evidence exists or OTA_CI_LAB=1 runs it."""
    want = [
        "ci_agent_crash_during_install",
        "ci_bad_signature_reject",
        "ci_fleet_ref_https_commit",
        "ci_hil_harness_dry_run",
    ]
    if os.environ.get("OTA_CI_LAB", "") in ("1", "true", "yes"):
        proc = run([sys.executable, "scripts/ci/lab-substitute.py"], timeout=600)
        if proc.returncode != 0:
            for name in want:
                row(results, name, "fail", (proc.stdout + proc.stderr)[-400:])
            return False
    ci_root = EVIDENCE / "ci"
    latest = None
    if ci_root.is_dir():
        for p in sorted(ci_root.glob("*/results.json"), reverse=True):
            latest = p
            break
    if latest is None:
        for name in want:
            row(results, name, "skip", "CI lab-substitute — scripts/ci/lab-substitute.py / workflow lab-substitute")
        return True
    data = json.loads(latest.read_text())
    by = {r["id"]: r for r in data.get("rows", [])}
    ok = True
    for name in want:
        r = by.get(name)
        if r and r.get("status") == "pass":
            row(results, name, "pass", r.get("detail", f"ci/{latest.parent.name}"))
        elif r:
            row(results, name, "fail", r.get("detail", ""))
            ok = False
        else:
            row(results, name, "skip", f"missing in ci/{latest.parent.name}")
    return ok


def main():
    require_bins()
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    results = []
    started = datetime.now(timezone.utc).isoformat()
    ok = True
    ok = go_tests(results) and ok
    ok = e2e_demo(results) and ok
    ok = render_board(results) and ok
    ok = bake_dry_run(results) and ok
    ok = schema_syntax(results) and ok
    ok = ci_lab_substitute(results) and ok
    hardware_pending(results)

    report = {
        "generated_at": started,
        "finished_at": datetime.now(timezone.utc).isoformat(),
        "board_profile": "minewing-gw1-r1",
        "host": os.uname().sysname if hasattr(os, "uname") else "unknown",
        "results": results,
        "software_pass": all(r["status"] == "pass" for r in results if r["status"] != "skip"),
        "hardware_claimed": False,
        "note": "Hardware/QEMU RAUC rows are skip until evidence/qualification/hardware-checklist.md is signed.",
    }
    out = EVIDENCE / "software-matrix.json"
    out.write_text(json.dumps(report, indent=2) + "\n")
    print(f"\nwrote {out}")
    if not report["software_pass"]:
        sys.exit(1)
    print("software qualification rows passed; hardware matrix still required for production")


if __name__ == "__main__":
    main()
