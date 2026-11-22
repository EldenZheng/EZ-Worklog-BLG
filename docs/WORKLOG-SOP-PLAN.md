# Plan: new worklog SOP (from 2026-11-21)

Status: the first version is built (`internal/shared/backend/sop.go`): push
on or after the entry's day (board Worklog Date/Owner still filled for now), SOP
worklogs read by issue search, commits that reference a Worklog count as
logged, and entries pending at the cutover tagged legacy (their Worklog links
the commits instead). Adding the Worklog's ref to already-pushed commits is
built too (`commit_rewrite.go`): rewritten in the clone named under Settings >
Repo folders, without checkout, force-pushed with a lease, journalled in
`data\rewrite-journal.json`. Commits not pushed yet are read from those clones
(`local_commits.go`, tagged Pending Push) and get their ref before their first
push, with no force-push. Merged commits are found before the push
(`merged_commits.go`), and a branch whose commits are all logged is offered a
pull request into its repository's default branch (`pull_request.go`). The git
hook is not built.
Entries dated before 2026-11-21 keep the current method.

Local clone discovery and commit scans share a maximum of two Git workers,
reduced to one on machines with one or two logical CPUs. Overlapping scans
share that budget so they do not each launch another set of Git processes.

## The new SOP

- Each piece of logged work is a **Worklog sub-issue** under the issue it
  belongs to (issue type `Worklog`).
- Minutes go in the issue field **Worklog Reported(mins)**.
- Up to **15 hours / 900 minutes** may be logged per day. Full logged minutes
  remain visible; payable time is capped at **480 minutes / 8 hours per day**,
  across all entries, for both Reported and Approved totals. Additional logged
  time does not increase that day's pay.
- Backfill is determined per Worklog by its **Logged for** date before daily
  totals and pay are capped. For example, 180 minutes created after midnight
  and marked for a previous day with 300 minutes completes that day's 480.
  Those minutes count once on that date. Unmarked excess remains on its
  creation day; it is not automatically spread across missing days.
- GitHub's **created date** cannot be backdated. Today's and past entries can
  be pushed; a late description starts with **Logged for YYYY-MM-DD**, a blank
  line, then the work details.
  Future entries wait until their date. SOP totals use the app's logged-for
  date, falling back to the Malaysia creation day when that note is absent.
- The worklog sub-issue must be **referenced by a commit** (the commit, not the
  parent issue, carries the ref).

## The flow

1. Commit as normal, with no ref. Push as normal.
2. In the app, draft the log as today: issue, day, minutes, linked to the
   commit(s). Today and past days can be pushed; a future draft waits for its day.
3. When pushed on or after the log's day, the app:
   1. creates the Worklog sub-issue under the issue and fills
      Worklog Reported(mins);
   2. adds the sub-issue's ref to the linked commit's message and rebases the
      branch from that commit;
   3. runs `git push --force-with-lease`;
   4. updates the commit SHAs it has saved in `worklog.csv`.

Example: Wednesday overtime, commit A drafted as 120m for Friday. A is pushed
Wednesday with no ref. Friday the app creates #6001, rewrites A to
`Refs owner/repo#6001`, rebases, force-pushes.

## App changes

- **Settings: repo folders.** Paste the folders that hold your clones. The app
  finds every subfolder with `.git` and maps it to `owner/repo` from its
  remote URL. Stored in `config.json`.
- **Local commit reading.** `git log` in those repos, so the app sees commits
  and their branch without waiting for GitHub.
- **Branch to issue.** Find the issue a branch belongs to: a branch naming rule
  (e.g. `5185-thing`) or the issue's linked branches via GraphQL.
- **Push creates a Worklog sub-issue** instead of a board item: issue type
  `Worklog`, parent set, Worklog Reported(mins) filled.
- **Forward drafts.** Drafts dated in the future become pushable on their day.
- **Past drafts.** Past entries can be pushed. Their description starts with
  `Logged for YYYY-MM-DD`, a blank line, then the work details, without changing
  remarks or duplicating the note on retry. Retries move the older footer note
  to the top while preserving commit links and the entry marker. Reads accept
  plain first-line dates on externally created Worklogs and older app footers.
- **Ref and rewrite.** Rewrite the message of the linked commit
  (scripted rebase from that commit), then `git push --force-with-lease`.
  Batch every log due that day into one rebase per branch.
- **SHA remap.** After a rewrite, replace old SHAs with new ones in
  `worklog.csv` so logged commits do not come back as unlogged (logged commits
  are tracked by SHA, `internal/shared/backend/store.go` `LoggedShas`).
- **Read back.** Status, Report, Log List read Worklog sub-issues (logged-for
  date, or Malaysia creation day without the app's note) from 2026-11-21 and
  the old board items before it. Reported and Approved totals use the same day.
  A separate description search finds late issues created after the requested
  range, including a previous month or payroll period, without downloading all
  subsequent worklogs. Local pushed rows fill the search-indexing delay once,
  on their intended date; approval stays zero until read from GitHub. The
  cut-over falls on a payroll period start, so no Report period mixes both.
- **Cut-over by date.** Entries before 2026-11-21 push the old way.

## Guards before a rewrite

Skip the repo and say why when:

- the branch is already merged into master (its commits are locked);
- the working tree has uncommitted or staged changes;
- a rebase, merge or cherry-pick is in progress;
- the local branch is behind its remote (pull first);
- the rebase would hit a conflict (abort, leave the branch as it was).

## Drawbacks

- **History is rewritten.** Every commit after the changed one gets a new SHA
  and is force-pushed. Safe only because one programmer owns each branch.
- **Duplicate timeline entries.** Later commits that already carried refs show
  twice on their issues' timelines (old and new SHA). GitHub keeps the old
  entries. Each rebase adds more; batching per day limits it.
- **Merge is a hard stop.** If the branch merges before the log's day, the
  forward log cannot get its commit ref.
- **Late creation dates.** A past entry creates its issue on the day it is
  pushed. Its description records the intended day used for app totals.
  GitHub board links filtered by Created date still follow the actual push day.
- **Visible gap.** The commit keeps its original author date (Wednesday) on a
  Friday worklog. Honest for a forward log, but reviewers see it.
- **Open PRs.** A force push shows on the PR, and review comments on old
  commits go stale.
- **Other clones.** A second machine on the same branch must reset after each
  force push.
- **Older commits cost more.** The older the linked commit, the more commits
  get rewritten and duplicated.

## Open questions

- Branch naming rule, or linked branches?
- Exact ref format the SOP expects (`Refs owner/repo#N`, `#N`, a full URL)?
- Exact names of the `Worklog` issue type and the Worklog Reported(mins) field,
  and whether every org has them.
- Who owns a log when one commit is split across several days?
