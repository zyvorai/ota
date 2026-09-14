---
hero:
  eyebrow: LAB
  title: Lab stack — deploy, wire, and verify
---

This page is the operator record for running Zyvor OTA **together** with
Fleet, Nodra, Device Agent, and relay-edge on one Linux host. It is **not**
board qualification. Simulator OTA never flashes disks or calls `reboot(2)`.

## Topology (recorded 14 September 2026)

Host `80.79.5.173` (`NLDW4-4-16-36`, Ubuntu 24.04, user `sus`). Existing
services already occupied `:8080` (Kryton), `:18080` (Yard), `:8081`
(kairon-node), so lab ports were chosen to avoid collisions.

```text
                    ┌─────────────────────────────────────────┐
                    │  Zyvor Fleet :18090 (HTTPS, demo)       │
                    │  UI + /api/v1 + /v1/devices OTA contract│
                    └──────────────┬──────────────────────────┘
           assignment/events       │ enroll + inventory
           (device bearer)         │ (zf_enroll_demo-local-only)
                    │              │
                    ▼              ▼
         zyvor-otad-demo      fleet-agent
         unix socket          --device-agent-url :9188
         simulator backend         │
                                   ▼
                    zyvor-device-agent :9188
                    nodra MQTT enabled ──────────► nodrad :1883
                                                    │
                                                    ▼
                                              nodra-server :18447

                    relay-edge :18086 (HTTPS UI / simulators)
                    publishes to Zyvor Relay when present
```

| Product | Unit | Listen | What the lab proved |
|---|---|---|---|
| Zyvor Fleet | `zyvor-fleet.service` | `https://80.79.5.173:18090/` | Control plane + OTA contract + site enroll |
| Fleet site agent | `zyvor-fleet-agent.service` | outbound to Fleet | Merged Device Agent inventory into site metadata |
| Nodra control plane | `nodra-server.service` | `http://80.79.5.173:18447/` | Dashboard, healthz, smoke-remote |
| Nodra edge | `nodrad.service` | MQTT `127.0.0.1:1883`, HTTP `127.0.0.1:9091` | Enrollment + MQTT ingress |
| Device Agent | `zyvor-device-agent.service` | `127.0.0.1:9188` | `nodra_connected=true`, fleet projection ready |
| OTA demo | `zyvor-otad-demo.service` | `/run/zyvor-ota-demo/agent.sock` | Signed Fleet assignment → committed |
| OTA artifacts | `zyvor-ota-demo-artifacts.service` | `127.0.0.1:58897` | Loopback bundle HTTP |
| relay-edge | `relay-edge.service` | `https://80.79.5.173:18086/ui/` | Health + fleet-module smoke |

Login (lab/demo only, rotate before any shared use):

- Fleet: `admin@zyvor.local` / `zyvor-fleet-demo`
- Nodra: `admin` / `nodra-lab-admin`

## Deploy each product

From each repository, as `sus` with SSH keys and passwordless sudo:

```sh
# Fleet (avoid :8080 if something else already owns it)
./scripts/deploy-remote.sh 80.79.5.173 sus --key --port 18090

# Nodra
./scripts/deploy-remote.sh 80.79.5.173 sus --port 18447

# relay-edge (direct publish; Relay itself may be absent)
RELAY_EDGE_DIRECT=1 EDGE_PORT=18086 ./scripts/deploy-remote.sh 80.79.5.173 sus

# OTA simulator smoke-deploy (runs make check && make demo on the host)
./scripts/deploy-remote.sh 80.79.5.173 sus --key

# Device Agent (bind stays loopback unless you pass --auth-mode bearer)
./scripts/deploy-remote.sh sus@80.79.5.173 --no-ui
```

OTA smoke-deploy is documented in [`deploy/README.md`](../deploy/README.md).
It is not a RAUC board image.

## Wire the stack

OTA requires **HTTPS** Fleet (`fleet_url` rejects `http://`). On the lab host:

1. Issue a local TLS cert for Fleet (`subjectAltName` includes `127.0.0.1`).
2. Restart `fleetd` with `--tls-cert` / `--tls-key`.
3. Trust that cert as `fleet_ca` on the OTA agent. The `zyvor-ota-demo` user
   must be able to **read** `/etc/zyvor-ota-demo/agent.json` (group
   `zyvor-ota-demo`, mode `640`). `ProtectSystem=strict` also requires
   `ReadWritePaths` only for state — config is read-only and must be
   world/group readable to the service user.
4. `POST /api/v1/ota/devices` as a Fleet admin; write the returned token to
   `/etc/zyvor-ota-demo/fleet.token` (mode `640`, group `zyvor-ota-demo`).
5. Set `fleet_url`, `fleet_token_file`, `fleet_ca` in the demo `agent.json`
   and restart `zyvor-otad-demo`.
6. `PUT /api/v1/ota/devices/{id}/assignment` with a **signed** envelope whose
   `device_id` matches the agent, `compatible`/`backend` match the config,
   and `sequence` is higher than the journal high-water mark.
7. Watch `zyvor-ota job JOB_ID` through commit. Simulator `auto_reboot` still
   only flips the JSON slot file.
8. `DELETE` the assignment after the job is terminal so the agent does not
   keep re-fetching it.

Fleet ACK of OTA events is **contiguous**. A synthetic `sequence: 1` posted
before a real job whose events start at `10` leaves a hole; the agent then
retains its outbox. Either start the Fleet device with `ackedSequence` equal
to the agent's last acknowledged sequence, or ACK locally with
`zyvor-ota ack SEQUENCE` only after Fleet has durably stored that prefix.

`nodrad` must persist enrollment next to its data directory (for example
`/var/lib/nodrad/nodrad.json`). A unit with `ProtectSystem=strict` cannot
rewrite `/etc/nodra` unless that path is in `ReadWritePaths`.

Enable Device Agent `[nodra] enabled = true` only after MQTT is listening,
then confirm:

```json
{"nodra_enabled":true,"nodra_connected":true,"fleet_enabled":true,"fleet_projection_ready":true}
```

Run `fleet-agent` with `--device-agent-url http://127.0.0.1:9188` and a
Fleet CA that the Go TLS stack trusts (install the lab cert into the host
CA store, or equivalent). Site inventory metadata then includes
`zyvor.device_agent.reachable` and `zyvor.device.serial`.

Helper on a host that already has the units: [`scripts/lab-wire-remote.sh`](../scripts/lab-wire-remote.sh)
(run **on** the lab machine as `sus`). Treat it as a bring-up sketch, not a
production installer.

## End-to-end result recorded on this host

| Path | Result |
|---|---|
| OTA selftest (deploy script) | 5/5 pass — signed install → reboot → commit |
| Fleet → OTA signed job `lab-wire-signed-1` / `-2` | `committed` (simulator slots) |
| Fleet event ACK after sequence alignment | `pending_events=0`, `ackedSequence` caught up |
| Fleet site `lab-nldw4` | `online`, Device Agent serial merged |
| Device Agent → Nodra MQTT | connected |
| relay-edge `smoke-fleet.sh` | PASS |
| relay-edge full `smoke.sh` publish into Relay | **502** — Zyvor Relay is not on this host (`:8443` free). Simulators and `/ui` still work. |

## What this lab does *not* prove

- Real `.raucb` install, bootloader attempt counters, or power-loss recovery.
- Fleet HA (v0.3 is single-writer).
- Production TLS identity (lab uses a self-signed Fleet cert).
- relay-edge → live Relay Accept path until Relay is deployed beside it.

Continue with [QUALIFICATION.md](QUALIFICATION.md) for the Minewing GW1 r1
hardware matrix and [PRODUCTION.md](PRODUCTION.md) for the factory-image path.
