# sensu-plugins-nginx

Sensu Go check reading the nginx [`stub_status`](https://nginx.org/en/docs/http/ngx_http_stub_status_module.html) page: nginx answers, the status page is well configured, and the connection counters stay under their thresholds.

Static Go binary (no CGO, no Ruby runtime): one asset for every Linux distribution, `amd64` only.

## Behavior

| Situation | State |
|---|---|
| HTTP 200 with a stub_status page, counters under their thresholds | OK |
| Active connections > `--active-connections` | WARNING |
| Waiting connections > `--waiting-requests` | WARNING |
| HTTP redirect (3xx, not followed) | WARNING (status endpoint misconfigured) |
| HTTP 200 but not a stub_status page | WARNING (status endpoint misconfigured) |
| Any other HTTP code (4xx, 5xx) | CRITICAL |
| Connection refused, timeout, DNS error | CRITICAL |
| Invalid argument (missing or invalid URL, negative threshold) | UNKNOWN |

Both thresholds are reported when both are exceeded. The output always carries the counters:

```
check-nginx-status OK: active 12, reading 0, writing 1, waiting 11
check-nginx-status WARNING: waiting requests 106 > 30 (active 291, reading 6, writing 179, waiting 106)
```

`Waiting` is the number of idle keep-alive connections, as reported by nginx.

## Usage

```
check-nginx-status [flags]

  -u, --url string               URL of the stub_status page, e.g. http://127.0.0.1/nginx-status (required)
  -a, --active-connections int   Warn when active connections exceed this value (default 300)
  -w, --waiting-requests int     Warn when waiting (idle keep-alive) connections exceed this value (default 30)
      --timeout int              HTTP request timeout in seconds (default 10)
```

Every option can also be set with an environment variable:

| Option | Variable | Annotation |
|---|---|---|
| `--url` | `CHECK_NGINX_URL` | `nginx_url` |
| `--active-connections` | `CHECK_NGINX_ACTIVE_CONNECTIONS` | `nginx_active_connections` |
| `--waiting-requests` | `CHECK_NGINX_WAITING_REQUESTS` | `nginx_waiting_requests` |
| `--timeout` | `CHECK_NGINX_TIMEOUT` | `nginx_timeout` |

The status page must be exposed by nginx, e.g.:

```nginx
server {
    listen 127.0.0.1:80;
    location = /nginx-status {
        stub_status;
        allow 127.0.0.1;
        deny all;
    }
}
```

Examples:

```sh
check-nginx-status -u http://127.0.0.1/nginx-status
check-nginx-status -u https://127.0.0.1:8443/status -a 1000 -w 200
```

## Check definition

Every option is passed through `env_vars`, filled per host from entity annotations with token substitution (applied by the agent to the whole check definition, `env_vars` included).

Naming rule: an annotation is named after its environment variable, lowercased, without the `CHECK_` prefix (`CHECK_NGINX_URL` → `nginx_url`).

- Every variable gets a non-empty default in the check definition, identical to the plugin default. Never use `| default ""` in `env_vars`: on a host missing the annotation the variable becomes `FOO=`, which Sensu rejects, and the agent silently drops the check (no event at all).
- The URL has no default in the plugin: the check definition provides it, so a host only needs an annotation when its status page lives elsewhere.

```yaml
---
type: CheckConfig
api_version: core/v2
metadata:
  name: check-nginx-status
spec:
  command: check-nginx-status
  env_vars:
    - CHECK_NGINX_URL={{ .annotations.nginx_url | default "http://127.0.0.1/nginx-status" }}
    - CHECK_NGINX_ACTIVE_CONNECTIONS={{ .annotations.nginx_active_connections | default "300" }}
    - CHECK_NGINX_WAITING_REQUESTS={{ .annotations.nginx_waiting_requests | default "30" }}
    - CHECK_NGINX_TIMEOUT={{ .annotations.nginx_timeout | default "10" }}
  runtime_assets:
    - opsone/sensu-plugins-nginx
  interval: 60
  timeout: 30
  publish: true
  subscriptions:
    - nginx
```

Annotations, all optional, e.g. in the agent configuration (`/etc/sensu/agent.yml`):

```yaml
annotations:
  nginx_url: http://127.0.0.1:8080/nginx-status   # default http://127.0.0.1/nginx-status
  nginx_active_connections: "1000"                # default 300
  nginx_waiting_requests: "200"                   # default 30
  nginx_timeout: "5"                              # seconds, default 10
```

## Migrating from the Ruby version (0.1.0)

- The command loses its `.rb` suffix: `check-nginx-status.rb` → `check-nginx-status`.
- `-h/--host`, `-p/--port` and `-q/--path` are replaced by a single `-u/--url`, required (the old default `http://127.0.0.1:80/nginx-status` now lives in the check definition). Example: `-h 10.0.0.1 -p 8080 -q status` → `-u http://10.0.0.1:8080/status`. HTTPS is supported.
- `--activecon` → `--active-connections`, `--waitingreq` → `--waiting-requests`; the `-a` and `-w` shorthands and their defaults (300, 30) are unchanged.
- Any redirect (3xx) is a WARNING, not only a 301.
- An HTTP 200 response that is not a stub_status page is a WARNING (it was OK).
- Both thresholds are reported at once, and the output carries the counters.
- New `--timeout`, 10 seconds by default (Ruby used the 60 seconds of `Net::HTTP`).
- The asset no longer depends on the Ruby runtime asset, nor on the Debian release.

## Build and release

```sh
make test          # go vet + unit tests
make integration   # runs the check against a real nginx in Docker
```

Release:

```sh
git tag 0.2.0 && git push origin 0.2.0
make release       # version taken from the tag, or forced: make release VERSION=0.2.0
sensuctl create -f dist/opsone-sensu-plugins-nginx_0.2.0.yml
```

`make release` runs `make dist`, which runs the tests then produces in `dist/`:

- one archive per architecture listed in `ARCHS` (`linux/amd64`) in Sensu asset layout (`bin/check-nginx-status`);
- the sha512 checksums file;
- `opsone-sensu-plugins-nginx_<version>.yml`, the asset definition pointing to the GitHub release of the tag.

It then creates the GitHub release of the tag with `gh`, uploads the archives and checksums, and uses the asset definition as release description.

`check-nginx-status version` prints the version, commit and build date.
