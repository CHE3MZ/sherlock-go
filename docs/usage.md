# Usage

## Install

Requires Go 1.21+.

```sh
# install to $GOPATH/bin as `sherlock`
go install github.com/CHE3MZ/sherlock-go/cmd/sherlock@latest

# or build locally
git clone https://github.com/CHE3MZ/sherlock-go.git
cd sherlock-go
go build -o sherlock ./cmd/sherlock
# same as: bash tools/build.sh
```

## Quick start

```sh
./sherlock john
./sherlock john alice bob
./sherlock john{?}        # checks john_, john-, john.
```

Results print as they come in. Only hits show by default — add `--print-all` to see misses too. Add `-v` for timing.

## Flags

Everything matches the Python version. ` --help` is the source of truth.

```
--site SITE           only check this site (repeat: --site GitHub --site GitLab)
--proxy URL, -p       proxy for all requests (http/https/socks5)
--timeout SECS        per-request timeout, default 60
--print-all           show misses and errors too
--print-found         show hits (default on)
--no-color            no colors (also respects NO_COLOR / non-tty)
-v, --verbose         show response time per site
--browse, -b          open hits in browser
--local, -l           use embedded site list, don't fetch live one
--json FILE, -j       local file, URL, or upstream PR number (e.g. 1234)
--nsfw                include NSFW sites
--ignore-exclusions   skip upstream false-positive filtering
--csv                 write <user>.csv
--xlsx                write <user>.xlsx in current dir
--txt                 write <user>.txt
--output FILE, -o     custom txt path (single user only)
--folderoutput DIR    put txt/csv in a folder (-fo)
--dump-response       print raw HTTP for debugging
```

Usernames go at the end. Put `{?}` inside a name for the wildcard.

## Output files

Nothing is written unless you ask for it.

* `txt` — hit URLs, one per line, ends with `Total Websites Username Detected On : N`
* `csv` — `username,name,url_main,url_user,exists,http_status,response_time_s`
* `xlsx` — one sheet called `sheet1`, URLs as hyperlinks

`--xlsx` always writes `./<username>.xlsx` even with `--folderoutput` — same quirk as Python.

## Site data

Embedded list is at `internal/site/data/data.json`.

* default — fetches live list from `https://data.sherlockproject.xyz`
* `--local` — uses the embedded copy
* `--json` — file path, URL, or PR number like `--json 123`

NSFW sites are skipped unless `--nsfw` (or you explicitly `--site` one). False positives from the upstream `exclusions` branch are filtered unless `--ignore-exclusions`.

## Proxy

```sh
./sherlock john --proxy socks5://127.0.0.1:1080
./sherlock john --proxy http://127.0.0.1:8080
```

Without `--proxy`, `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` from the environment are used.

## Examples

```sh
# just GitHub and GitLab, save everything
./sherlock torvalds --site GitHub --site GitLab --csv --txt --xlsx --print-all

# a few names into a folder
./sherlock alice bob --folderoutput results --csv

# scripting friendly
./sherlock john --timeout 10 --no-color --print-all | grep "Not Found"
```
