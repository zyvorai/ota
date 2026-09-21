---
hero:
  eyebrow: LAB
  title: Lab stack — deploy, wire, and verify
---

This page is the operator record for running Zyvor OTA **together** with
Fleet, Nodra, Device Agent, and relay-edge on one Linux host. It is **not**
Minewing board qualification. Simulator OTA never flashes disks or calls
`reboot(2)`. Generic QEMU RAUC lab HIL is documented separately in
[HIL.md](HIL.md) Track A.

## Topology (updated 14–15 September 2026)

Host `80.79.5.173` (`NLDW4-4-16-36`, Ubuntu 24.04, user `sus`). Existing
services already occupied `:8080` (Kryton), `:18080` (Yard), `:8081`
(kairon-node), so lab ports were chosen to avoid collisions.

```text
                    ┌─────────────────────────────────────────┐
                    │  Zyvor Fleet :18090 (HTTPS, non-demo)   │
                    │  ZYVOR_FLEET_DEMO=0 · OTA contract      │
                    └──────────────┬──────────────────────────┘
           assignment/events       │ enroll + inventory
           (device bearer)         │
                    │              │
                    ▼              ▼
         zyvor-otad-demo      fleet-agent
         unix socket          --device-agent-url :9188
         simulator backend         │
                                   ▼
                    zyvor-device-agent :9188
                    (lab-surrogate · not Minewing HIL)
                    nodra MQTT ──────────────────► nodrad :1883
                                                    │
                                                    ▼
                                         nodra-server :18447 (HTTPS)

                    relay-edge :18086 (HTTPS · EDGE_REQUIRE_AUTH=1)
```

| Product | Unit | Listen | Lab posture (signed where noted) |
|---|---|---|---|
| Zyvor Fleet | `zyvor-fleet.service` | `https://80.79.5.173:18090/` | Non-demo ExecStart; TLS; ops + abbreviated WAN signed (Fleet `ops-checklist.md`) |
| Fleet site agent | `zyvor-fleet-agent.service` | outbound to Fleet | Merged Device Agent inventory into site metadata |
| Nodra control plane | `nodra-server.service` | `https://80.79.5.173:18447/` | Direct TLS (`NODRA_TLS_*`); ops + abbreviated WAN/disk signed |
| Nodra edge | `nodrad.service` | MQTT `127.0.0.1:1883`, HTTP `127.0.0.1:9091` | Enrollment + MQTT ingress |
| Device Agent | `zyvor-device-agent.service` | `127.0.0.1:9188` | **Lab-surrogate only** — `minewing_claimable=false`; not silicon HIL |
| OTA demo | `zyvor-otad-demo.service` | `/run/zyvor-ota-demo/agent.sock` | Signed Fleet assignment → committed (simulator) |
| OTA artifacts | `zyvor-ota-demo-artifacts.service` | `127.0.0.1:58897` | Loopback bundle HTTP |
| relay-edge | `relay-edge.service` | `https://80.79.5.173:18086/ui/` | `auth_required=true`; ops checklist signed |

Login (lab credentials — rotate before any shared use):

- Fleet: `admin@zyvor.local` / password from `/etc/zyvor-fleet/fleet.env`
- Nodra: `admin` / password from `/etc/nodra/nodra.env`
- relay-edge: `Authorization: Bearer <EDGE_API_TOKEN>` (token not in git)

## Deploy each product

From each repository, as `sus` with SSH keys and passwordless sudo:

```sh
# Fleet (avoid :8080 if something else already owns it)
./scripts/deploy-remote.sh 80.79.5.173 sus --key --port 18090
# Then ensure non-demo: ZYVOR_FLEET_DEMO=0 and no --demo on ExecStart

# Nodra (enable NODRA_TLS_CERT / NODRA_TLS_KEY for HTTPS)
./scripts/deploy-remote.sh 80.79.5.173 sus --port 18447

# relay-edge with auth required
EDGE_API_TOKEN=… EDGE_REQUIRE_AUTH=1 RELAY_EDGE_DIRECT=1 EDGE_PORT=18086 \
  ./scripts/deploy-remote.sh 80.79.5.173 sus

# OTA simulator smoke-deploy
./scripts/deploy-remote.sh 80.79.5.173 sus --key

# Device Agent (loopback / lab-surrogate — not Minewing claim)
./scripts/deploy-remote.sh sus@80.79.5.173 --no-ui
```

OTA smoke-deploy is documented in [`deploy/README.md`](https://github.com/zyvorai/ota/blob/main/deploy/README.md).
It is not a RAUC board image.

## Wire the stack

OTA requires **HTTPS** Fleet (`fleet_url` rejects `http://`). On the lab host:

1. Issue a local TLS cert for Fleet (`subjectAltName` includes `127.0.0.1`).
2. Restart `fleetd` with `--tls-cert` / `--tls-key` (**without** `--demo`).
3. Trust that cert as `fleet_ca` on the OTA agent. The `zyvor-ota-demo` user
   must be able to **read** `/etc/zyvor-ota-demo/agent.json` (group
   `zyvor-ota-demo`, mode `640`).
4. `POST /api/v1/ota/devices` as a Fleet admin; write the returned token to
   `/etc/zyvor-ota-demo/fleet.token` (mode `640`, group `zyvor-ota-demo`).
5. Set `fleet_url`, `fleet_token_file`, `fleet_ca` in the demo `agent.json`
   and restart `zyvor-otad-demo`.
6. `PUT /api/v1/ota/devices/{id}/assignment` with a **signed** envelope whose
   `device_id` matches the agent, `compatible`/`backend` match the config,
   and `sequence` is higher than the journal high-water mark.
7. Watch `otactl job JOB_ID` through commit. Simulator `auto_reboot` still
   only flips the JSON slot file.
8. `DELETE` the assignment after the job is terminal so the agent does not
   keep re-fetching it.

Fleet ACK of OTA events is **contiguous**. A synthetic `sequence: 1` posted
before a real job whose events start at `10` leaves a hole; the agent then
retains its outbox. Either start the Fleet device with `ackedSequence` equal
to the agent's last acknowledged sequence, or ACK locally with
`otactl ack SEQUENCE` only after Fleet has durably stored that prefix.

Enable Device Agent `[nodra] enabled = true` only after MQTT is listening,
then confirm `nodra_connected=true`. Treat Device Agent on this host as
**lab-surrogate** evidence only ([device-agent HIL.md](https://github.com/zyvorai/device-agent/blob/main/docs/HIL.md)).

Helper: [`scripts/lab-wire-remote.sh`](https://github.com/zyvorai/ota/blob/main/scripts/lab-wire-remote.sh)
(run **on** the lab machine as `sus`). Bring-up sketch, not a production installer.

## End-to-end result recorded on this host

| Path | Result |
|---|---|
| OTA selftest (deploy script) | 5/5 pass — signed install → reboot → commit (simulator) |
| Fleet → OTA signed job | `committed` (simulator slots) |
| Fleet event ACK | `pending_events=0` after sequence alignment |
| Fleet site `lab-nldw4` | `online`, Device Agent serial merged |
| Device Agent → Nodra MQTT | connected (surrogate host) |
| relay-edge with auth | `auth_required=true`; unauthorized `/v1/*` → 401 |
| Generic QEMU RAUC lab HIL | **Complete** — [HIL.md](HIL.md) Track A / `qemu-lab/CHECKLIST.md` |

## What this lab does *not* prove

- Minewing GW1 r1 silicon / BSP image claims (`minewing_rauc_claimable`).
- Fleet or Nodra HA (single-writer products).
- Production public TLS identity (lab uses self-signed certs).
- relay-edge → live Relay Accept until Relay is deployed beside it (`:8443`).

Continue with [QUALIFICATION.md](QUALIFICATION.md) for the Minewing matrix and
[PRODUCTION.md](PRODUCTION.md) for the factory-image path.
