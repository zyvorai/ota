---
hero:
  eyebrow: MEASUREMENTS
  title: Simulator and relay figures
  lead: One run of make measure on a developer machine. These are not RAUC flash times, not a QEMU lab, and not Minewing silicon.
---

`make measure` runs `go run ./scripts/measure`. It drives the in-process simulator and a localhost relay, then prints JSON. The file [benchmarks/simulator.json](benchmarks/simulator.json) is the output of the run below. A later run will differ by milliseconds and by resident-set size. Do not copy these figures onto a board result.

| | |
|---|---|
| When | 2026-09-21T11:15:58Z |
| Host | Darwin 25.6.0 arm64, go1.27.1 |
| Profile | simulator |
| QEMU | not measured; `QUALIFY_QEMU_IMAGE` was unset |
| Minewing | not measured; no board run |

`rss_kb_peak` is the macOS `ps` resident set, in kilobytes, of the measure process during that phase. The process includes the Go runtime and the artifact bytes held for the localhost download. It is not `zyvor-otad` on a device. At process start, before the scenarios, resident set was 17104 KB.

## Install

A 1048576-byte schema 1 artifact was signed, downloaded from localhost, installed by the simulator, and committed. `health_stable_seconds` was 0. The booted slot was `rootfs.1`.

| Field | Value |
|---|---|
| Wall time | 75 ms |
| Steps | 7 |
| Cache | 1048576 bytes |
| `ota.db` | 4096 bytes |
| `ota.db-wal` | 70072 bytes |
| State directory | 1155734 bytes |
| Resident set peak | 22560 KB |

## Rollback

The health check failed. `health_timeout_seconds` was 1, and the wall time includes that deadline. The simulator then booted `rootfs.0` and the job was `rolled_back`.

| Field | Value |
|---|---|
| Wall time | 1090 ms |
| Steps | 186 |
| Resident set peak | 25520 KB |

## NeedsRecovery

The simulator failed the install. The job reached `needs_recovery` in 29 ms (3 steps). `recover-abort` then finished in 15 ms and left the job `failed`. That call is in-process. It is not a board power cycle.

Resident set peak during this phase was 26608 KB.

## Relay

One hundred clients requested the same 1048576-byte artifact through the localhost relay. Upstream was read once.

| Field | Value |
|---|---|
| Upstream fetches | 1 |
| Upstream bytes | 1048576 |
| Client bytes | 104857600 |
| Bytes not read from upstream again | 103809024 |
| Wall time | 44 ms |
| Resident set peak | 30960 KB |

The relay was on localhost. The byte counts are the upstream read and the bytes delivered to clients, not a measurement of a wide-area link.
