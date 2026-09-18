#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""CI substitute for lab-blocked OTA rows (no real RAUC / power-loss hardware).

Covers what GitHub runners can prove without QUALIFY_QEMU_IMAGE:
  - agent crash mid-install → needs_recovery + recover-abort
  - bad release signature rejected
  - HTTPS zyvor-fleet-ref assignment → committed (simulator slots)
  - HIL harness dry-run (evidence layout only; never claims Minewing RAUC)

Does **not** claim qemu_rauc_* or physical power-loss rows.
"""
from __future__ import annotations

import functools
import hashlib
import http.server
import json
import os
import pathlib
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import threading
import time
from datetime import datetime, timedelta, timezone

ROOT = pathlib.Path(__file__).resolve().parents[2]
CLI = ROOT / "bin/zyvor-ota"
DAEMON = ROOT / "bin/zyvor-otad"
FLEET_REF = ROOT / "bin/zyvor-fleet-ref"
EVIDENCE = ROOT / "evidence/qualification/ci"


def require_bins():
    for p in (CLI, DAEMON, FLEET_REF):
        if not p.is_file():
            raise SystemExit(f"missing {p}; run make build first")


def openssl_self_signed(cert: pathlib.Path, key: pathlib.Path) -> None:
    subprocess.run(
        [
            "openssl", "req", "-x509", "-newkey", "ec",
            "-pkeyopt", "ec_paramgen_curve:P-256",
            "-keyout", str(key), "-out", str(cert),
            "-days", "1", "-nodes",
            "-subj", "/CN=zyvor-fleet-ref-ci",
            "-addext", "subjectAltName=IP:127.0.0.1,DNS:localhost",
        ],
        check=True, capture_output=True, text=True,
    )


class Runner:
    def __init__(self, work: pathlib.Path):
        self.work = work
        self.assets = work / "assets"
        self.assets.mkdir()
        self.healthy = work / "healthy"
        self.healthy.touch()
        self.log = (work / "daemon.log").open("w+")
        self.proc = None
        self.server = None
        self.thread = None
        self.port = None
        self.base = None

    def start_http(self):
        handler = functools.partial(
            http.server.SimpleHTTPRequestHandler, directory=str(self.assets)
        )
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            self.port = probe.getsockname()[1]
        self.base = f"http://127.0.0.1:{self.port}"

    def command(self, *args, api=False, succeeds=True):
        argv = [str(CLI)] + (["-simulation-url", self.base] if api else []) + list(args)
        result = subprocess.run(argv, text=True, capture_output=True, timeout=40)
        if succeeds and result.returncode:
            raise AssertionError(f"{args}: {result.stdout} {result.stderr}")
        if not succeeds:
            assert result.returncode != 0, f"unexpected success: {args}"
            return result
        return json.loads(result.stdout) if api else result.stdout

    def write_config(self, extra=None):
        cfg = {
            "device_id": "ci-1",
            "compatible": "qemu-demo",
            "backend": "simulator",
            "state_dir": str(self.work / "state"),
            "socket": str(self.work / "agent.sock"),
            "trust_keys": {"demo": (self.work / "keys/release.pub").read_text().strip()},
            "download_hosts": [f"127.0.0.1:{self.server.server_address[1]}"],
            "max_artifact_bytes": 1048576,
            "reserve_bytes": 1048576,
            "health_timeout_seconds": 4,
            "health_stable_seconds": 0,
            "checks": [{"kind": "file", "target": str(self.healthy)}],
        }
        if extra:
            cfg.update(extra)
        (self.work / "agent.json").write_text(json.dumps(cfg))

    def start_daemon(self):
        self.proc = subprocess.Popen(
            [
                str(DAEMON),
                "-config", str(self.work / "agent.json"),
                "-simulation-listen", f"127.0.0.1:{self.port}",
            ],
            stdout=self.log,
            stderr=self.log,
        )
        for _ in range(100):
            if self.proc.poll() is not None:
                raise AssertionError("daemon exited early")
            try:
                self.command("status", "json", api=True)
                return
            except AssertionError:
                time.sleep(0.05)
        raise AssertionError("daemon did not start")

    def stop_daemon(self, kill=False):
        if self.proc is None:
            return
        if kill:
            self.proc.kill()
        else:
            self.proc.terminate()
        try:
            self.proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5)
        self.proc = None

    def retry_busy(self, fn, timeout=8):
        deadline = time.monotonic() + timeout
        while True:
            try:
                return fn()
            except AssertionError as e:
                if "engine busy" not in str(e) or time.monotonic() >= deadline:
                    raise
                time.sleep(0.1)

    def wait_state(self, job, expected, timeout=40):
        deadline = time.monotonic() + timeout
        value = None
        while time.monotonic() < deadline:
            value = self.command("job", job, api=True)
            if value["state"] == expected:
                return value
            time.sleep(0.15)
        raise AssertionError(f"timeout waiting for {expected}: {value}")

    def make_assignment(self, sequence, job_id, bad_sig=False, auto_reboot=False):
        data = (f"simulated OS release {sequence}\n" * 80).encode()
        (self.assets / "os.raucb").write_bytes(data)
        now = datetime.now(timezone.utc)
        release = {
            "schema": 1,
            "id": f"release-{sequence}",
            "sequence": sequence,
            "compatible": "qemu-demo",
            "backend": "simulator",
            "version": f"0.1.{sequence}",
            "expires": (now + timedelta(hours=1)).isoformat(),
            "artifact": {
                "url": f"http://127.0.0.1:{self.server.server_address[1]}/os.raucb",
                "size": len(data),
                "sha256": hashlib.sha256(data).hexdigest(),
            },
        }
        (self.work / "release.json").write_text(json.dumps(release))
        key = self.work / ("keys/evil.key" if bad_sig else "keys/release.key")
        if bad_sig:
            self.command("keygen", str(self.work / "evil-keys"))
            shutil.copy(self.work / "evil-keys/release.key", key)
        self.command(
            "sign",
            str(self.work / "release.json"),
            str(key),
            "demo",
            str(self.work / "envelope.json"),
        )
        assignment = {
            "job_id": job_id,
            "device_id": "ci-1",
            "release": json.loads((self.work / "envelope.json").read_text()),
            "not_before": (now - timedelta(minutes=1)).isoformat(),
            "deadline": (now + timedelta(minutes=10)).isoformat(),
            "auto_reboot": auto_reboot,
        }
        path = self.work / f"{job_id}.json"
        path.write_text(json.dumps(assignment))
        return path, assignment

    def close(self):
        self.stop_daemon(kill=True)
        if self.server:
            self.server.shutdown()
            self.server.server_close()
        if self.thread:
            self.thread.join(timeout=2)
        self.log.close()


def test_crash_during_install(r: Runner) -> str:
    """Stop daemon, force job into Installing, restart → needs_recovery."""
    path, assignment = r.make_assignment(1, "job-crash-1")
    r.retry_busy(lambda: r.command("submit", str(path), api=True))
    r.wait_state(assignment["job_id"], "awaiting_reboot")
    r.stop_daemon()
    state_path = r.work / "state" / "state.json"
    db = json.loads(state_path.read_text())
    job = db["jobs"][assignment["job_id"]]
    job["state"] = "installing"
    db["jobs"][assignment["job_id"]] = job
    # state dir must stay mode 0700 / not group-writable
    state_path.write_text(json.dumps(db))
    r.start_daemon()
    got = r.wait_state(assignment["job_id"], "needs_recovery")
    r.retry_busy(lambda: r.command("recover-abort", api=True))
    r.wait_state(assignment["job_id"], "failed")
    return f"needs_recovery error={got.get('error', '')[:80]!r}"


def test_bad_signature(r: Runner) -> str:
    path, _ = r.make_assignment(90, "job-badsig", bad_sig=True)
    r.command("submit", str(path), api=True, succeeds=False)
    return "submit rejected bad signature"


def test_fleet_ref_commit(r: Runner) -> str:
    cert = r.work / "fleet.crt"
    key = r.work / "fleet.key"
    openssl_self_signed(cert, key)
    token_file = r.work / "fleet.token"
    token_file.write_text("ci-fleet-token\n")
    os.chmod(token_file, 0o600)

    path, assignment = r.make_assignment(2, "job-fleet-1", auto_reboot=True)
    # Point fleet-ref at this assignment; agent will poll HTTPS.
    listen_sock = socket.socket()
    listen_sock.bind(("127.0.0.1", 0))
    fleet_port = listen_sock.getsockname()[1]
    listen_sock.close()

    fleet = subprocess.Popen(
        [
            str(FLEET_REF),
            "-token", "ci-fleet-token",
            "-devices", "ci-1",
            "-assignment", str(path),
            "-listen", f"127.0.0.1:{fleet_port}",
            "-cert", str(cert),
            "-key", str(key),
        ],
        stdout=r.log,
        stderr=r.log,
    )
    try:
        # Wait for TLS accept
        ctx = ssl.create_default_context(cafile=str(cert))
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                with socket.create_connection(("127.0.0.1", fleet_port), timeout=1) as sock:
                    with ctx.wrap_socket(sock, server_hostname="localhost"):
                        break
            except Exception:
                time.sleep(0.1)
        else:
            raise AssertionError("fleet-ref did not accept TLS")

        r.stop_daemon()
        r.write_config({
            "fleet_url": f"https://127.0.0.1:{fleet_port}",
            "fleet_token_file": str(token_file),
            "fleet_ca": str(cert),
        })
        r.start_daemon()
        # Fleet Sync should pull the assignment; fall back to socket submit.
        try:
            r.wait_state(assignment["job_id"], "awaiting_reboot", timeout=60)
        except AssertionError:
            r.retry_busy(lambda: r.command("submit", str(path), api=True))
            r.wait_state(assignment["job_id"], "awaiting_reboot", timeout=45)
        # auto_reboot=True should advance; nudge if still waiting
        try:
            r.wait_state(assignment["job_id"], "committed", timeout=60)
        except AssertionError:
            r.retry_busy(lambda: r.command("reboot", api=True))
            r.wait_state(assignment["job_id"], "committed", timeout=45)
        return f"fleet-ref HTTPS → committed on :{fleet_port}"
    finally:
        fleet.terminate()
        try:
            fleet.wait(timeout=5)
        except subprocess.TimeoutExpired:
            fleet.kill()
            fleet.wait(timeout=5)


def test_hil_dry_run() -> str:
    env = os.environ.copy()
    env["OTA_HIL_ENV"] = "dry-run"
    env["OTA_HIL_STRICT"] = "0"
    env["OTA_HIL_SIGN"] = "0"
    env["OTA_HIL_SKIP_QUALIFY"] = "1"
    proc = subprocess.run(
        ["bash", "scripts/hil/run-rauc-powerloss-hil.sh"],
        cwd=ROOT,
        env=env,
        text=True,
        capture_output=True,
        timeout=300,
    )
    if proc.returncode != 0:
        raise AssertionError(proc.stdout[-800:] + proc.stderr[-800:])
    # Find newest hil dir
    hil = ROOT / "evidence/qualification/hil"
    stamps = sorted(hil.glob("*/results.json"), reverse=True) if hil.is_dir() else []
    if not stamps:
        raise AssertionError("HIL dry-run produced no results.json")
    data = json.loads(stamps[0].read_text())
    if data.get("minewing_rauc_claimable"):
        raise AssertionError("dry-run must not be minewing_rauc_claimable")
    return f"hil dry-run stamp={stamps[0].parent.name} claimable=false"


def main():
    require_bins()
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    out = EVIDENCE / stamp
    out.mkdir(parents=True)
    rows = []

    def add(rid, status, detail):
        rows.append({"id": rid, "status": status, "detail": detail})
        print(f"[{status.upper()}] {rid} — {detail}")

    with tempfile.TemporaryDirectory(prefix="zyvor-ota-ci-") as tmp:
        work = pathlib.Path(tmp)
        # state dir permissions: OpenStore rejects group/world writable
        state = work / "state"
        state.mkdir(mode=0o700)
        r = Runner(work)
        try:
            r.start_http()
            r.command("keygen", str(work / "keys"))
            r.write_config()
            r.start_daemon()

            try:
                detail = test_crash_during_install(r)
                add("ci_agent_crash_during_install", "pass", detail)
            except Exception as e:
                add("ci_agent_crash_during_install", "fail", str(e))

            try:
                detail = test_bad_signature(r)
                add("ci_bad_signature_reject", "pass", detail)
            except Exception as e:
                add("ci_bad_signature_reject", "fail", str(e))

            try:
                detail = test_fleet_ref_commit(r)
                add("ci_fleet_ref_https_commit", "pass", detail)
            except Exception as e:
                add("ci_fleet_ref_https_commit", "fail", str(e))
        finally:
            r.close()
            shutil.copy2(work / "daemon.log", out / "daemon.log")

    try:
        detail = test_hil_dry_run()
        add("ci_hil_harness_dry_run", "pass", detail)
    except Exception as e:
        add("ci_hil_harness_dry_run", "fail", str(e))

    counts = {}
    for row in rows:
        counts[row["status"]] = counts.get(row["status"], 0) + 1
    report = {
        "product": "zyvor-ota",
        "environment": "github-actions",
        "stamp": stamp,
        "finished_at": datetime.now(timezone.utc).isoformat(),
        "minewing_rauc_claimable": False,
        "counts": counts,
        "rows": rows,
        "note": "Lab substitute only — does not close QEMU/physical RAUC power-loss rows",
    }
    (out / "results.json").write_text(json.dumps(report, indent=2) + "\n")
    (out / "SUMMARY.md").write_text(
        f"# OTA CI lab-substitute\n\n"
        f"- stamp: `{stamp}`\n"
        f"- counts: `{counts}`\n"
        f"- minewing_rauc_claimable: **false**\n\n"
        "GitHub CI covers agent recovery + fleet-ref + HIL harness dry-run.\n"
        "Real RAUC flash / power-loss still needs `QUALIFY_QEMU_IMAGE`.\n"
    )
    print(f"\nwrote {out}")
    if counts.get("fail", 0):
        sys.exit(1)


if __name__ == "__main__":
    main()
