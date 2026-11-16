# AGENTS.md — home utility meter reader

Context for agents working on this setup. Last verified: 2026-11-16.

## What this is

A Raspberry Pi (hostname `metric`, IP `10.22.14.174`) with an RTL-SDR dongle
(Realtek RTL2838, USB `0bda:2838`) reading utility meter ERT radio broadcasts
(~912 MHz) using a fork of [rtlamr](https://github.com/bemasher/rtlamr) that
adds a Prometheus exporter (`-format=prometheus`). This repo is that fork
(`turbohoje/rtlamr-prometheus`; exporter code in `flags.go`).

## Meters

| ID | Protocol | ERT type | Commodity | Notes |
|---|---|---|---|---|
| **98883030** | SCM+ | 156 (0x9C) | **Gas** (Itron 100G DLS per `meters.csv`) | **The user's gas meter.** Heard every ~15–90 s. Read 247440 on 2023-05-11, 446543 on 2026-10-17, 449510 on 2026-11-16. |
| 70339819 | SCM+ | 171 (0xAB) | Water (probably) | Not in `meters.csv`. The low nibble 0xB = 11 matches the water type (Itron 100W). Rarely heard (~1/day). Read 490501 in Nov 2026. Might not be the user's meter. |
| ~~3806796~~ | SCM | 7 | Electric (Itron 45ES/C1SR family) | **Retired.** The user confirmed it can no longer be read this way. Removed from the filter on 2026-11-16. Last known reading 1150769 (May 2023). |

ERT type → commodity reference: `meters.md` / `meters.csv` in this repo.

Copies of both systemd unit files are in `deploy/systemd/`. If you change a unit in `/etc/systemd/system/`, update the copy too.

## Running services (systemd)

- `rtltcp.service`: `/usr/local/bin/rtl_tcp -a 0.0.0.0` (port 1234). It owns the dongle.
- `rtlprometheus.service`: `Requires=rtltcp.service`. Runs
  `/usr/local/bin/rtlamr -filterid=98883030,70339819 -msgtype=scm,scm+ -format=prometheus`
  - Serves metrics at `http://10.22.14.174:8080/metrics` (port 8080 is hard-coded in `flags.go`), gauge
    `home_metered_consumption{id=...,type=...}`
  - Logs each reading to the journal: `journalctl -u rtlprometheus -n 20`
  - `/usr/local/bin/rtlamr` was built from this repo (May 2023). `~/go/bin/rtlamr` is upstream rtlamr.

Quick health check:
```sh
systemctl is-active rtltcp rtlprometheus
curl -s localhost:8080/metrics | grep home_metered
```

## Prometheus / Grafana (not running)

- `~/docker.sh` holds the old `docker run` commands. Config is at
  `/mnt/files/docker/prometheus/prometheus.yml` (NFS from the NAS at `10.22.14.3`).
- The scrape target in that config is `10.22.14.5:8080`, which is **stale**. The Pi is now `10.22.14.174`.
- As of 2026-11-16, no Prometheus or Grafana container or process is running on the Pi.
  Only stopped/exited containers from 2023–2024 remain. The exporter runs on its own.

## Other files in ~

- `~/rtlamr`: upstream rtlamr checkout. `~/rtlamr-pi`, `~/rtl-sdr`: earlier builds.
- `~/3030.txt`: May 2023 capture of the gas meter. `~/out.txt`: May 2023 capture of all nearby meters.
- `~/rtlamer_wrap.py`, `~/test.py`, `~/othertest.py`: early experiments (rtl_tcp/pyrtlsdr tests), not used.
- Tuning seen in captures: center freq 912600155 Hz, sample rate 2359296.
