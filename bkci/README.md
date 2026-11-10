# BK-CI standalone collector

This branch starts at upstream **Telegraf v1.40.3**, not at the previous BK-CI fork implementation. The custom executable lives in `cmd/bk-ci-collector`. Agent and collector are independently built and released. No Agent Go module is linked into this executable.

## Build and publish

Requirements: Go 1.27.x (validated with 1.27.3) and Git; use the existing Agent Make/Task environment. No Python packaging step is required. Module versions are pinned in the upstream module files; the deployed `gopsutil/v4 v4.24.5` is deliberately preserved by a replace directive for the compatibility inputs.

From the sibling Agent module directory (`../agent`), its normal `build_*` targets emit the collector alongside Agent, daemon and upgrader. Collector-only examples:

```sh
make collector_build COLLECTOR_OS=windows COLLECTOR_ARCH=amd64 COLLECTOR_VERSION=1.40.3-bkci.2
make collector_build COLLECTOR_OS=linux COLLECTOR_ARCH=amd64 COLLECTOR_VERSION=1.40.3-bkci.2
make collector_build COLLECTOR_OS=darwin COLLECTOR_ARCH=arm64 COLLECTOR_VERSION=1.40.3-bkci.2
```

These invoke Go directly and produce only the platform executable in the Agent `bin` directory. There is no archive, sidecar metadata or manifest-merge stage. External editions use `BUILD_OUT_TAG=out` and a separate repository directory/manifest. Do not mix inner and out binaries under one manifest: their legacy field/measurement translations differ.

Native macOS releases should use `COLLECTOR_CGO=1` on a macOS builder to retain the existing Foundation available-capacity correction. Cross-compiling with CGO disabled retains the previous no-CGO fallback, not the Foundation behavior.

Sign the executable using the existing pipeline if required, upload that exact binary to an internal HTTP(S) repository, then update the release manifest with its version and platform URL. URLs may be absolute or relative to the manifest; optional `sha256`/`size` are checked only when present. The Agent downloads the executable directly and uses it when the download succeeds; failures are logged and retried on the next Agent startup. Publish a new version for every content change and make binaries available before publishing the manifest. A repository example and operational details are in the parent project's `docs/agent/collector-release.md`.

For an offline initial installation copy the executable to:


```text
<Agent workdir>/components/telegraf/bundled/bk-ci-collector.exe
```

Omit `.exe` on Unix. The Agent never overwrites a running executable. Downloaded releases use `components/telegraf/versions/<version>-<digest>/`. Current and previous good versions are retained. Upgrades are checked once at Agent startup, not at every collector restart.

## Agent properties

```properties
devops.agent.collectorOn=true
devops.agent.collector.manifest.url=https://YOUR-INTERNAL-REPOSITORY/collector/inner/manifest.json
devops.agent.collector.config=
devops.agent.collector.cgroup.parent=
```

The repository URL must be set to your actual address; no production address is embedded. Without a URL or a bundled/downloaded executable, the optional collector is disabled with a warning and builds continue. Local property changes take effect on the next Agent startup. The backend sends the manifest URL configured on the Agent's gateway row as `collectorManifestUrl` in install packages and heartbeat responses; null preserves the local value. A heartbeat update never triggers an immediate download or restart. The existing `collectorOn` switch is now persisted and respected.

`collector.config` defaults to `<workdir>/collector.conf`. An absent default file means no custom inputs. An explicitly configured missing/invalid file is an error rather than silently losing requested metrics. An IP change restarts the existing collector with new startup metadata without another upgrade check.

## Runtime contract

Agent starts exactly one automatic collector. stdin supplies one newline-delimited JSON bootstrap message (schema 1), followed by an open pipe. This is a small runtime envelope, not a metrics format. EOF stops the collector. Agent identity and credentials are not command-line arguments and are not written into a generated TOML file. Stdout returns one readiness message; no collected metrics return to Agent. `--test -d 1s` is a separate diagnostic mode that prints measurements without reporting.

All system algorithms and platform mappings were moved to `internal/bkci/compat`; they are wrapped by `inputs.bkci_compat`. Telegraf owns scheduling, non-reentrancy, processors, bounded output buffers, serialization, retries and flushes. A stalled Gather is not re-entered. The old scheduler/reporter exist only in `_test.go` files as compatibility test oracles, never in either production executable.

Windows LibreHardwareMonitor is enabled by default as another compatibility input. HTTP-first/WMI fallback, two-probe policy, temperature normalization and filtering were retained. No LHM driver or LHM executable is bundled. Its readings remain `env.temperature` with the existing sensor/hardware tags.

The ordinary project output is native `outputs.http` JSON (`{"metrics":[...]}`, Unix seconds). Stream has two independent native HTTP outputs: Influx line protocol to the current `/agents/metrix` endpoint and filtered JSON CPU-total/memory/disk snapshots to `/agents/metrics`. There is **no send ordering contract**. Global identity and hardware tags match current Agent behavior; no implicit `host` or `agentSecret` tag is added.

## Third-party inputs

Copy/adapt `bkci/collector.example.conf` to `collector.conf`. The extension file is standard Telegraf TOML using the custom bounded `[[inputs.bkci_external]]` plugin. Each instance accepts exactly one HTTP URL or one executable argument array. Parsers are native Telegraf `influx`, `json` and `prometheus` implementations. The raw unrestricted HTTP/exec plugins are deliberately not linked into this reduced binary.

Use `mapping="fields"` for JSON/Influx field names. For Prometheus metric version 1 use `mapping="measurement_field"`: the original measurement becomes a field name before changing the measurement to `env`; `gauge/counter/value` generic field names are not mistaken for the user metric name. `field_prefix` optionally namespaces fields. Labels remain tags. Prometheus version 2 can use `mapping="fields"`. All custom measurements remain `env`, including `out` builds; system rename rules never touch these inputs.

Limits: 16 extension inputs, at least 10-second intervals, at most 4 concurrent third-party collections, timeout 3 seconds by default and at most 30 seconds, 1 MiB HTTP/stdout/stderr reads, 1,024 metrics per collection, 64 fields and 32 tags per metric, bounded field/tag lengths. Telegraf buffers 1,024 metrics per output in batches of 256. Saturated input slots skip a sample rather than accumulating queued work. Oversized input is rejected; output buffers may drop old metrics under sustained delivery failure.

## Resource and lifecycle behavior

Windows uses a separate Job Object with kill-on-close, 256 MiB aggregate committed-memory limit, a 10% machine CPU cap, and at most 16 processes including descendants. The collector must receive its bootstrap only after assignment. Scripts start suspended, are assigned to their own nested Job Object and then resumed, so timeout termination includes their descendants. No build process is assigned to these jobs. The collector executable is independent of `devopsAgent.exe`, avoiding its Windows self-upgrade file lock.

Linux requires cgroup v2 delegation supporting `cgroup.kill` and `clone3`/`CLONE_INTO_CGROUP`. Set `collector.cgroup.parent` to a writable delegated directory with CPU/memory/pids controllers enabled for children. Defaults to `/sys/fs/cgroup`. Limits are 256 MiB memory, no swap, 10% of one core, and 128 tasks (threads count). Scripts use child cgroups. If these guarantees cannot be established, **only the collector is disabled**; Agent builds continue. There is no silent unrestricted fallback and no attempt to alter host cgroup/system configuration automatically. This is a deployment prerequisite for Linux, not a guarantee that every existing host is already configured.

macOS uses low-priority process groups, `GOMAXPROCS=1`, a 128 MiB Go heap target and a best-effort 256 MiB self RSS watchdog. It does not provide an OS-enforced aggregate hard cap. Script extensions are disabled on macOS in this first implementation; bounded HTTP extensions and system inputs remain available.

The parent closes stdin on normal stop and waits at most 10 seconds before hard termination. The collector has its own 8-second cancellation watchdog for blocked OS/plugin calls. Parent process death on Windows closes the Job Object; on Linux stdin EOF/SIGTERM triggers child cleanup. Every restart waits for old process termination; a stuck teardown never overlaps a second instance. Crash recovery is supervised by Suture v4 with a jittered two-second backoff on every failure and stops after five failures per Agent lifetime. Suture does not replace Job/cgroup containment or release rollback; process cleanup completes before a replacement is allowed. New releases must complete readiness plus 10 seconds of probation before promotion; failed versions are persisted and not downloaded repeatedly. This is a startup health check, not proof that the remote backend is reachable.

## Logs and diagnosis

Collector runtime logs: `logs/collector.log`. Diagnostic sessions: `logs/collector-diagnostic.log`. Each is capped at 10 MiB plus three rotated files. Existing `.debug` toggling and `logs/monitor_metrics.log` moved into the collector, capped at 10 MiB plus three backups. Separate files prevent diagnostic and background processes rotating the same log. Agent only logs component lifecycle status.

```powershell
devopsAgent.exe monitor -d 2s
```

This uses an installed collector and never triggers a download. A 45-second diagnostic deadline prevents a stalled plugin from holding the CLI forever. No metrics are reported in diagnostic mode.

## Reduced plugin inventory

Only compatibility inputs, bounded URL/script input, debug processor, HTTP output and JSON/Influx/Prometheus formats are imported. The `bkci_lite` build tag removes optional AWS/GCP authentication dependencies from the native HTTP output; ordinary upstream builds retain them. Both variants bound successful response consumption to 64 KiB. Further plugins require a new compiled release, not merely a configuration edit.

## Verification

```powershell
go test -p 2 -short -tags bkci_lite ./internal/bkci/... ./cmd/bk-ci-collector
go test -p 2 -short -tags out,bkci_lite ./internal/bkci/... ./cmd/bk-ci-collector
```

Tests cover migrated system/Windows/LHM algorithms, old wire-contract oracles, actual native HTTP output against a loopback server, snapshot filtering, Prometheus-to-env mapping, response bounds and script timeout. Agent-side `component` tests cover download/length/checksum, rollback/rejected releases, state retention, readiness and stdin EOF. An explicit `BKCI_TEST_COLLECTOR` environment variable enables the real subprocess-to-loopback-backend test; it never connects to a production BK-CI endpoint.

Before production rollout additionally test Linux delegation and macOS native-CGO behavior on target hosts, actual backend compatibility, all supported hardware classes, Agent service stop/uninstall/self-upgrade, low disk space, parent force-kill and descendant cleanup, and long-running CPU/RSS profiles under build load. A local Windows compile/test is not a substitute for this platform rollout matrix.
