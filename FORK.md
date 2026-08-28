# What this fork changes

`README.md` is upstream's and is left alone, so a pull never has to be merged
into it. Everything this fork does differently is here.

Upstream logs work; this fork reads it back. One install covers a whole team:
the tabs are filtered to a **profile**, and the picker above them switches
between people.

## Profiles

A profile is one person — a name, a GitHub login, and a salary. The login is
what the project boards are filtered on; the salary is what the Report's day
rate comes from. Everything else (the repos, the boards) is shared.

| Field | Notes |
| --- | --- |
| Name | What the picker shows. Falls back to the login if left blank. |
| GitHub login | The value in the board's *worklog-owner* field. The dropdown is filled from the members of the orgs in Settings; a login it cannot see can still be typed. |
| Base salary per 21 days | A period pays for 21 working days regardless of what the calendar says. |

Switching profiles throws the cached months away and refetches. Keeping them
would put one person's hours on screen under another person's name and salary,
which reads exactly like a real answer.

The org member list comes from `gh api orgs/<org>/members`, fetched once, the
first time Settings is opened. It is not a search: `search/users` has no prefix
operator, so `blg- in:login` matches the substring anywhere and drags in a
hundred unrelated accounts from other orgs. Membership is the team.

An install that predates profiles turns its existing username and salary into
the first profile on next launch. Nothing has to be re-entered.

## Settings

Two boxes, and the profiles list:

| Field | Notes |
| --- | --- |
| Repos or orgs to scan | Comma-separated. Also where the login dropdown gets its org from. |
| Worklog project URLs | One per line. Status and Report read all of them. |

Upstream's other fields — currency, look-back window, default push mode,
Anthropic API key — belong to the writing half of the app and are not shown.
They are **carried, not cleared**: a save copies `ui.cfg` and only overwrites
what is on screen, so whatever `config.json` already holds for them survives.
Un-hiding the Log work tab therefore needs no re-configuring.

To change or clear one — including an Anthropic key saved by an older build —
edit `config.json` by hand.

The Report shows one currency and no conversion. The USD tile, the rate
footnote and the "Fetch exchange rate" button are all gone.

## It reads, it does not write

Every GitHub call goes through `gh`. With the Log work tab off the bar there is
no button left that writes: Status and Report only run `gh api` reads against
the boards, and the dropdown only reads org membership. Nothing is created,
edited or pushed.

The write code is unreachable, not deleted. `pushToProject`, `setFields`,
`setStatusDone` and `addSubIssue` are all still there; the only path to them was
`entryTable` → `rowTile` → `pushRow`, and `entryTable` is called once, inside
`drawRecent`, which is Log work. Put the tab back and the push path works
exactly as upstream's does.

`gh` is still required, with all three scopes. `project` is needed to **read**
ProjectV2, not just to write it:

```
gh auth login
gh auth refresh -s project
```

## Local files

Upstream keeps its data next to the executable (see README). This fork adds one
file there:

| File | Contents |
| --- | --- |
| `profiles.json` | the people you can switch between, and which one is showing |

**It holds salaries. Never commit it.** It is in `.gitignore`, along with
upstream's three and the `specs.txt` below.

## Staying in sync with upstream

Nearly all of the fork is in two new files upstream will never touch:

| File | What |
| --- | --- |
| `profiles.go` | the `Profile` type, `profiles.json`, the picker, the Settings tab, org members |
| `profiles_test.go` | 11 tests for the above |

What is left in files upstream owns is deliberately small:

| File | Change |
| --- | --- |
| `main.go` | one `UI` field; `buildAllTabs` drops the Log work tab and calls `buildForkSettingsTab`; the startup fetch opens Status instead of Log work; `drawReport` loses the conversion tile and the rate footnote; two `refreshRate` calls removed |
| `chart.go` | `statusTabIndex` is `0`, not `1`, now that Log work is off the bar |
| `.gitignore` | `profiles.json` and `specs.txt` added |

`buildSettingsTab` in `main.go` is **byte-identical to upstream** and unused —
`buildForkSettingsTab` in `profiles.go` replaced it rather than patching it, so
upstream can rewrite its version freely and the merge is a no-op.

`refreshRate` and `fetchRate` are likewise untouched and now only reached by
upstream's own tests. Left in place for the same reason.

If a pull conflicts, `main.go` and `chart.go` are the only places to look.

## Building on a username with a space

`gcc` on Windows pulls in `default-manifest.o` through a path built from its own
install location. When that path contains a space the linker splits it:

```
ld.exe: cannot find C:/Users/Your: No such file or directory
```

Reinstalling the toolchain somewhere without a space is the real fix. To get a
build out without touching the install, copy the spec file and cut the manifest
out of it:

```
gcc -dumpspecs > specs.txt
sed -i 's/%{!shared:%:if-exists(default-manifest\.o%s)}//' specs.txt
CGO_LDFLAGS="-specs=$PWD/specs.txt" go build -ldflags "-H windowsgui -s -w" -o Worklog.exe .
```

The same `CGO_LDFLAGS` is needed for `go test`, which links a second binary.
`specs.txt` is gitignored.
