#!/usr/bin/env python3
"""Run the real daemon and CLI against isolated simulator files, never hardware.

Tests keygen/sign, HTTP download, durable restart before reboot, commit,
health-triggered rollback, event ACK, replay rejection, and cache cleanup.
Python standard library only. Run `make build` first.
"""
import functools
import hashlib
import http.server
import json
import pathlib
import socket
import subprocess
import tempfile
import threading
import time
from datetime import datetime, timedelta, timezone

ROOT = pathlib.Path(__file__).resolve().parents[1]
CLI = ROOT / "bin/otactl"
DAEMON = ROOT / "bin/zyvor-otad"


def run():
    with tempfile.TemporaryDirectory(prefix="zyvor-ota-e2e-") as tmp:
        work = pathlib.Path(tmp)
        assets = work / "assets"
        assets.mkdir()
        healthy = work / "healthy"
        healthy.touch()
        proc = None
        log = (work / "daemon.log").open("w+")
        handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(assets))
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            port = probe.getsockname()[1]
        base = f"http://127.0.0.1:{port}"

        def command(*args, api=False, succeeds=True):
            argv = [str(CLI)] + (["-simulation-url", base] if api else []) + list(args)
            result = subprocess.run(argv, text=True, capture_output=True, timeout=35)
            if succeeds and result.returncode:
                raise AssertionError(f"{args}: {result.stdout} {result.stderr}")
            if not succeeds:
                assert result.returncode != 0, "unexpected command success"
                return
            return json.loads(result.stdout) if api else result.stdout

        def start():
            nonlocal proc
            proc = subprocess.Popen([str(DAEMON), "-config", str(work / "agent.json"),
                                     "-simulation-listen", f"127.0.0.1:{port}"], stdout=log, stderr=log)
            for _ in range(100):
                if proc.poll() is not None:
                    raise AssertionError("daemon exited")
                try:
                    command("status", "json", api=True)
                    return
                except AssertionError:
                    time.sleep(0.05)
            raise AssertionError("daemon did not start")

        def stop():
            nonlocal proc
            if proc is not None:
                proc.terminate()
                proc.wait(timeout=10)
                assert proc.returncode == 0, "unclean shutdown"
                proc = None

        def retry_busy(fn, timeout=5):
            # Submit/Reboot/GC all take the same lock the once-per-second engine
            # tick holds briefly, so an occasional transient "engine busy" is
            # expected contention, not a failure of the operation itself.
            deadline = time.monotonic() + timeout
            while True:
                try:
                    return fn()
                except AssertionError as e:
                    if "engine busy" not in str(e) or time.monotonic() >= deadline:
                        raise
                    time.sleep(0.1)

        def wait_state(job, expected):
            deadline = time.monotonic() + 35
            while time.monotonic() < deadline:
                value = command("job", job, api=True)
                if value["state"] == expected:
                    return value
                if value["state"] in {"failed", "needs_recovery"}:
                    raise AssertionError(value)
                time.sleep(0.15)
            raise AssertionError(f"timeout waiting for {expected}: {value}")

        try:
            command("keygen", str(work / "keys"))
            config = {
                "device_id": "demo-1", "compatible": "qemu-demo", "backend": "simulator",
                "state_dir": str(work / "state"), "socket": str(work / "agent.sock"),
                "trust_keys": {"demo": (work / "keys/release.pub").read_text().strip()},
                "download_hosts": [f"127.0.0.1:{server.server_port}"],
                "max_artifact_bytes": 1048576, "reserve_bytes": 1048576,
                "health_timeout_seconds": 4, "health_stable_seconds": 0,
                "checks": [{"kind": "file", "target": str(healthy)}],
            }
            (work / "agent.json").write_text(json.dumps(config))
            start()
            for sequence in [1, 2]:
                data = (f"simulated OS release {sequence}\n" * 100).encode()
                (assets / "os.raucb").write_bytes(data)
                now = datetime.now(timezone.utc)
                release = {"schema": 1, "id": f"release-{sequence}", "sequence": sequence,
                           "compatible": "qemu-demo", "backend": "simulator", "version": f"0.1.{sequence}",
                           "expires": (now + timedelta(hours=1)).isoformat(),
                           "artifact": {"url": f"http://127.0.0.1:{server.server_port}/os.raucb",
                                        "size": len(data), "sha256": hashlib.sha256(data).hexdigest()}}
                (work / "release.json").write_text(json.dumps(release))
                command("sign", str(work / "release.json"), str(work / "keys/release.key"),
                        "demo", str(work / "envelope.json"))
                assignment = {"job_id": f"job-{sequence}", "device_id": "demo-1",
                              "release": json.loads((work / "envelope.json").read_text()),
                              "not_before": (now - timedelta(minutes=1)).isoformat(),
                              "deadline": (now + timedelta(minutes=10)).isoformat(), "auto_reboot": False}
                path = work / "assignment.json"
                path.write_text(json.dumps(assignment))
                retry_busy(lambda: command("submit", str(path), api=True))
                wait_state(assignment["job_id"], "awaiting_reboot")
                if sequence == 1:
                    stop()
                    start()
                    assert command("job", "job-1", api=True)["state"] == "awaiting_reboot"
                else:
                    healthy.unlink()
                retry_busy(lambda: command("reboot", api=True))
                wait_state(assignment["job_id"], "committed" if sequence == 1 else "rolled_back")
                healthy.touch()
                assignment["job_id"] = f"replay-{sequence}"
                path.write_text(json.dumps(assignment))
                command("submit", str(path), api=True, succeeds=False)
            events = command("events", api=True)
            command("ack", str(events[-1]["sequence"]), api=True)
            assert not command("events", api=True)
            retry_busy(lambda: command("gc", api=True))
            print("PASS: real daemon/CLI simulator E2E (commit, restart, rollback, replay, events, GC)")
        except Exception:
            log.flush()
            log.seek(0)
            print(log.read())
            raise
        finally:
            stop()
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)
            log.close()


if __name__ == "__main__":
    run()
