# Worklog

A desktop app for logging work against GitHub issues and reading the result
back off a GitHub Project board.

It scans the commits you have pushed but not yet logged, turns each one into a
worklog entry, and pushes that entry to the project board as a sub-issue with
the date, minutes, owner and remarks filled in. The Status and Report tabs then
read those entries back from the board — a calendar of the month, and a payroll
period totalled against a daily target.

Built with [Fyne](https://fyne.io). All GitHub access goes through the
[`gh` CLI](https://cli.github.com), so the app never handles a token itself.

## Tabs

| Tab | What it does |
| --- | --- |
| **Log work** | Commits you have pushed but not logged, as tiles. Tap one to log it. Also takes manual meeting/other entries. |
| **Status** | A month calendar; each day fills towards the daily target, coloured by organisation. |
| **Report** | A payroll period — the 21st to the 20th — as stat tiles, a per-day bar chart, and the split by organisation. |
| **Settings** | Everything below. |

Tapping an organisation in any colour key hides its work across all three tabs.

## Requirements

- **Go 1.26** or newer
- **A C toolchain.** Fyne uses cgo, so a working `gcc` has to be on `PATH`.
  On Windows, [MSYS2/MinGW-w64](https://www.msys2.org) or
  [WinLibs](https://winlibs.com) — installed to a path with **no spaces in it**.
  A gcc under `C:\Users\Your Name\...` passes an unquoted path to the linker and
  every cgo build dies with `ld.exe: cannot find C:/Users/Your`.
- **`gh`**, authenticated. Two commands, not one:

  ```
  gh auth login
  gh auth refresh -s project
  ```

  The token needs `repo`, `read:org` and `project`. `gh auth login` grants the
  first two but **not** `project` — that is the scope the board is read and
  written through, so without the refresh the Log work tab still finds commits
  and every push then fails. Confirm with `gh auth status`; all three scopes
  should be listed.

## Build and run

```
git clone <this repo> worklog-go
cd worklog-go
go build -o Worklog .
./Worklog
```

On Windows, drop the console window that would otherwise open behind the app:

```
go build -ldflags "-H windowsgui -s -w" -o Worklog.exe .
```

> **Use `go build`, not `go run .`** — the app keeps its data in the directory
> the binary sits in, and `go run` builds into a temporary directory that is
> deleted afterwards, so everything you log is thrown away on exit. If you want
> `go run` for development, set `WORKLOG_DIR` first (see below).

### Where the data goes

In order of preference:

1. `$WORKLOG_DIR`, if it is set
2. the directory the executable is in
3. the current working directory

Three files are created there on first use. All three are in `.gitignore` and
none of them belong in a commit:

| File | Contents |
| --- | --- |
| `config.json` | your settings — **including the Anthropic API key, if you set one** |
| `worklog.csv` | every entry you have logged, remarks and all |
| `ignored.json` | the commit shas you have deliberately kept off the pending list |

## First run

The app starts empty and asks for nothing at launch — open **Settings** and
fill it in. Nothing is stored anywhere but the directory above; there is no
account and no server.

| Field | Notes |
| --- | --- |
| GitHub username | Optional. Blank means whoever `gh` is logged in as. |
| Worklog owner | The value written to the board's *worklog-owner* field, and what Status and Report filter on. |
| Repos or orgs to scan | Comma-separated. A bare `myorg` scans every repo in the org; `myorg/one-repo` scans just that one. |
| Worklog project URLs | One per line, e.g. `https://github.com/orgs/myorg/projects/9`. Status and Report read all of them. |
| Base salary per 21 days | Drives the Report tab's rate. A period pays for 21 working days regardless of what the calendar says. |
| Currency / Also show in | The second is converted with a rate fetched from `open.er-api.com`, at most once a day. |
| Look back (days) | How far back the pending-commit scan goes. |
| Default push mode | `subissue` creates a sub-issue under the referenced issue; `issue` writes the fields onto that issue directly. |
| Anthropic API key | Optional, only used by "Compact with AI" when remarks run past the board's 1024-character field limit. |

### What the board needs

The project the issues sit on must carry fields whose names start with
`Worklog` — the app matches on `worklog` + `date`, `owner`, `min` and `remark`,
case-insensitively. A `Status` field with a `Done` option is used if present.
Without those fields an entry still pushes, and the app says which ones it could
not write.

## Tests

```
go test ./...
```

104 tests, all offline — nothing reaches GitHub, so a fresh clone runs green in
under a second. On Windows, if `gcc` is not the first one on `PATH`, point cgo
at the right one:

```
CC=C:/mingw64/bin/gcc.exe go test ./...
```

`live_probes_test.go` is the exception, and is behind a build tag for that
reason: it calls real GitHub with whichever org, project and issue were being
worked on at the time. Edit the refs to something on your own account before
running it.

```
go test -tags live -run Probe -v
```

`ld.exe: .rsrc merge failure: multiple non-default manifests` during a Windows
build is harmless — the binary is still produced. It comes from the embedded
icon resource in `rsrc_windows_*.syso`.
