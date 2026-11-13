# Changelog

## [0.3.0] - 2026-11-13

### Added
- `--writing-connections` (`CHECK_NGINX_WRITING_CONNECTIONS`, default 200): warns on requests being processed, the load counter.
- `--load-grace` (`CHECK_NGINX_LOAD_GRACE`, default 180 seconds): a threshold exceeded is only reported once it lasts this delay; within it the check stays OK and tells it (`… for 1m0s, within grace`).
- `--state-dir` (`CHECK_NGINX_STATE_DIR`, default `/var/cache/sensu/sensu-agent`): state file of the grace delay. Without usable state file, thresholds exceeded are reported at once with a `grace WARNING` line.

### Changed
- `--waiting-requests` / `CHECK_NGINX_WAITING_REQUESTS` renamed `--waiting-connections` / `CHECK_NGINX_WAITING_CONNECTIONS` (shorthand `-w` kept), disabled by default (was 30): idle keep-alive connections are no load.
- A threshold set to 0 disables its test.
- Output says `waiting connections` instead of `waiting requests`.

## [0.2.1] - 2026-11-08

### Changed
- Repository cleanup (removed `CLAUDE.md` and an obsolete Ruby-era `.gitignore` rule); no functional change.

## [0.2.0] - 2026-11-08

### Changed
- Rewritten in Go with the Sensu plugin SDK: static binary for `linux/amd64`, no Ruby runtime asset, one asset for every Debian release.
- Command renamed without the `.rb` suffix (`check-nginx-status`).
- `-h/--host`, `-p/--port` and `-q/--path` replaced by `-u/--url` (`CHECK_NGINX_URL`), required: the `http://127.0.0.1/nginx-status` default moves to the check definition.
- `--activecon` renamed `--active-connections`, `--waitingreq` renamed `--waiting-requests` (shorthands `-a`/`-w` and defaults unchanged).
- Any HTTP redirect (3xx) is a WARNING, not only 301; redirects are not followed.
- An HTTP 200 response that is not a stub_status page is a WARNING instead of OK.
- Both thresholds are reported when exceeded, and the output carries the stub_status counters.

### Added
- Every option can be set through an environment variable (`CHECK_NGINX_*`).
- `--timeout` (default 10 seconds).
- HTTPS status pages.

## [0.1.0]

### Added
- Ruby check `check-nginx-status.rb`.
