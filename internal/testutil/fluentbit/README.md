# Fluent Bit Test Utility

This directory contains the local Fluent Bit Forward test service.

## Start and Stop

Run these commands from the repository root:

```bash
task fluentbit:start
task fluentbit:status
task fluentbit:restart
task fluentbit:stop
```

Fluent Bit runs in the tmux session `directive-proxy-fluentbit`. Repeating
`task fluentbit:start` is safe; an existing session is reused.

The Forward input listens on `0.0.0.0:23194`. Records are written as JSON to
`output/events.log`.

The tmux process ID is stored in `fluent-bit.pid` at this directory's root.
The entire `output/` directory is ignored and can be deleted whenever a clean
output file is needed.

## Configuration Reload

After editing `config.yaml` or an included pipeline file, trigger a hot reload:

```bash
task fluentbit:reload
```

This calls `POST /api/v2/reload` on the local management endpoint. Hot reload
is enabled by `service.hot_reload: on`.

## HTTP API

The management API listens only on `127.0.0.1:23193`.

```bash
# Service/version overview
curl http://127.0.0.1:23193/

# Overall health and failed-output counters
curl http://127.0.0.1:23193/api/v2/health

# Input, filter, and output counters as JSON
curl http://127.0.0.1:23193/api/v1/metrics

# Prometheus exposition format
curl http://127.0.0.1:23193/api/v1/metrics/prometheus

# Memory/filesystem buffering and chunks
curl http://127.0.0.1:23193/api/v1/storage

# Trigger a hot reload
curl -X POST -d '' http://127.0.0.1:23193/api/v2/reload

# Number of completed hot reloads
curl http://127.0.0.1:23193/api/v2/reload

# Extended v2 metrics
curl http://127.0.0.1:23193/api/v2/metrics
```

The API is intended for local development and diagnostics. Keep the bind
address at `127.0.0.1` unless access from another host is explicitly needed.
