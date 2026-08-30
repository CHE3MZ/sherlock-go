# Architecture

## Layout

```
cmd/sherlock/     CLI entrypoint, flag parsing, wiring
internal/cli/     colors, arg reordering, browser
internal/engine/  request pipeline and detection
internal/site/    manifest loading and filtering
internal/report/  result types and terminal styling
internal/output/  txt / csv / xlsx writers
```

No third-party dependencies. Just the standard library.

## How a scan works

1. `site.Load` gets the site list — live fetch by default, embedded with `--local`, or custom via `--json`. It also applies the upstream exclusions list and NSFW filtering.
2. `engine.Run` fires requests with up to 20 concurrent workers. Each site gets the right method (`HEAD` for `status_code`, `GET` otherwise, or explicit `request_method`), headers, and interpolated URL/payload. Username spaces are encoded as `%20` for URLs only.
3. As each response comes back, it is classified and streamed to the terminal via `report.Notifier`. The final `results` slice stays in manifest order for the file writers, but terminal output is in completion order so you see hits immediately.
4. `output` writes `txt` / `csv` / `xlsx` only if requested.

HTTP is forced to 1.1 (like Python `requests`) and uses a shared cookie jar. `HTTP_PROXY` env vars are respected when no explicit `--proxy` is given.

## Detection

Each site declares an `errorType`:

* `status_code` — 2xx is a hit, otherwise miss (with optional `errorCode` list)
* `message` — miss if `errorMsg` appears in the body
* `response_url` — miss if the request redirects (redirects are disabled to check)

WAF fingerprints (Cloudflare, AWS, PerimeterX) are checked first and surface as `Blocked by bot detection`.

Username `regexCheck` is enforced before any request. A few upstream patterns use lookarounds that Go's RE2 doesn't support — those are translated to equivalent RE2 patterns in `internal/engine/engine.go`.

## Site manifest

`internal/site/data/data.json` is the embedded copy. Live data is fetched at runtime unless `--local` or `--json` is used. Required fields per site are `url`, `urlMain`, `errorType`, and `username_claimed`. The loader preserves JSON key order so file outputs stay deterministic.

## Output

`report.Notifier` replicates Python's `colorama` styling byte-for-byte (including autoreset handling). `output.WriteTxt` / `WriteCSV` / `WriteXLSX` match Python's formats — CSV uses CRLF, XLSX uses hyperlink formulas.

## Differences from Python

* No update check that pings `sherlock-project` releases
* HTTP/1.1 only — avoids noisy HTTP/2 HEAD errors
* Streaming instead of batching

Otherwise the logic and file formats are identical.

## Development

```sh
go vet ./...
go test ./...
go test -race ./...
bash tools/build.sh   # -> ./sherlock
go run ./cmd/sherlock --help
```
