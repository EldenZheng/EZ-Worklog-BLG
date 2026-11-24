package backend

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// A commit that is already merged can never be given its Worklog's reference:
// rewriting it would rewrite the default branch, or a branch whose pull request
// is closed. merged_refs lists an entry's commits known to be merged, so the
// entry shows Already merged while it is still a draft, its Worklog links them
// in its description (WorklogIssueBody) and the rewrite leaves them alone.
//
// The check reads GitHub only, so it needs no clone. It runs when the pending
// commits are fetched, after a draft is saved, and again just before a push;
// the rewrite's own check after the push stays as the last word.

var hexSha = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// mergedChunk is how many commits one GraphQL request asks about.
const mergedChunk = 30

// RowCommitRefs is r's commits, with the repo filled in for rows written
// before refs carried one.
func RowCommitRefs(r Row) []CommitRef {
	var out []CommitRef
	for _, c := range ParseCommitRefs(r["refs"]) {
		if c.Repo == "" {
			if o, n, _, err := SplitIssue(r["issue"]); err == nil {
				c.Repo = o + "/" + n
			}
		}
		out = append(out, c)
	}
	return out
}

func commitKey(c CommitRef) string {
	return strings.ToLower(strings.TrimSpace(c.Repo)) + "@" + strings.ToLower(strings.TrimSpace(c.Sha))
}

func mergedSet(r Row) map[string]bool {
	set := map[string]bool{}
	for _, c := range ParseCommitRefs(r["merged_refs"]) {
		set[commitKey(c)] = true
	}
	return set
}

// UnmergedRefs is r's commits that are not known to be merged.
func UnmergedRefs(r Row) []CommitRef {
	merged := mergedSet(r)
	var out []CommitRef
	for _, c := range RowCommitRefs(r) {
		if !merged[commitKey(c)] {
			out = append(out, c)
		}
	}
	return out
}

// AllRefsMerged reports whether r has commits and every one is merged.
func AllRefsMerged(r Row) bool {
	return strings.TrimSpace(r["refs"]) != "" && strings.TrimSpace(r["merged_refs"]) != "" && len(UnmergedRefs(r)) == 0
}

// needsMergedCheck reports whether r has commits that may still be given its
// Worklog's reference, so whether they are merged matters.
func needsMergedCheck(r Row) bool {
	return SOPApplies(r["date"]) && !RowIsLegacy(r) && strings.TrimSpace(r["refs"]) != "" &&
		r["ref_state"] != RefComplete && len(UnmergedRefs(r)) > 0
}

// legacyMayClear reports whether r is a legacy entry that may lose the tag:
// under the SOP, not pushed yet, with commits. Every entry waiting at the SOP's
// start was tagged legacy once, merged or not; one with commits still unmerged
// can be given its Worklog's reference like any other, so the merged check
// clears its tag (RefreshMergedRefs, CheckRowMerged).
func legacyMayClear(r Row) bool {
	return RowIsLegacy(r) && SOPApplies(r["date"]) && strings.TrimSpace(r["refs"]) != "" &&
		strings.TrimSpace(r["pushed_at"]) == "" && strings.TrimSpace(r["issue_url"]) == ""
}

// mergeCheckWanted is every entry whose commits' merged state matters.
func mergeCheckWanted(r Row) bool { return needsMergedCheck(r) || legacyMayClear(r) }

// applyMerged records merged on r and, when the check was complete, clears a
// legacy tag whose entry still has an unmerged commit. It reports a change.
func applyMerged(r Row, merged map[string]bool, complete bool) bool {
	changed := false
	if list, ok := withMerged(r, merged); ok {
		r["merged_refs"] = list
		changed = true
	}
	if complete && legacyMayClear(r) && len(UnmergedRefs(r)) > 0 {
		r["legacy"] = ""
		changed = true
	}
	return changed
}

// MergedCommits reports which of refs are merged: on their repo's default
// branch, or part of a merged pull request (which also catches squash and
// rebase merges, whose original commits never reach the default branch). One
// or two requests per repo per mergedChunk commits. A repo that cannot be read is left
// out and its error returned, after the others are checked.
func MergedCommits(refs []CommitRef) (map[string]bool, error) {
	byRepo := map[string][]CommitRef{}
	var repos []string
	for _, c := range refs {
		if c.Repo == "" || !hexSha.MatchString(c.Sha) {
			continue
		}
		key := strings.ToLower(c.Repo)
		if byRepo[key] == nil {
			repos = append(repos, key)
		}
		byRepo[key] = append(byRepo[key], c)
	}
	merged := map[string]bool{}
	var firstErr error
	for _, key := range repos {
		list := byRepo[key]
		for start := 0; start < len(list); start += mergedChunk {
			end := min(start+mergedChunk, len(list))
			if err := mergedInRepo(list[start:end], merged); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return merged, firstErr
}

// mergedInRepo asks in two steps. A commit GitHub does not have (not pushed
// yet, or its branch deleted) reads back as null from object(), but fails the
// whole request in compare(), so only the commits found are compared.
func mergedInRepo(list []CommitRef, merged map[string]bool) error {
	owner, name, ok := strings.Cut(list[0].Repo, "/")
	if !ok {
		return nil
	}
	var objects strings.Builder
	for i, c := range list {
		fmt.Fprintf(&objects, " c%d:object(expression:%q){... on Commit{associatedPullRequests(first:5){nodes{merged}}}}", i, c.Sha)
	}
	repo, err := mergedQuery(list[0].Repo, owner, name, objects.String())
	if err != nil {
		return err
	}
	var compare []CommitRef
	for i, c := range list {
		var obj *struct {
			AssociatedPullRequests struct {
				Nodes []struct {
					Merged bool `json:"merged"`
				} `json:"nodes"`
			} `json:"associatedPullRequests"`
		}
		if raw, ok := repo[fmt.Sprintf("c%d", i)]; ok {
			_ = json.Unmarshal(raw, &obj)
		}
		if obj == nil {
			continue
		}
		inMerged := false
		for _, pr := range obj.AssociatedPullRequests.Nodes {
			inMerged = inMerged || pr.Merged
		}
		if inMerged {
			merged[commitKey(c)] = true
		} else {
			compare = append(compare, c)
		}
	}
	if len(compare) == 0 {
		return nil
	}
	// A commit pushed straight to the default branch has no pull request: the
	// default branch is the base, and a commit it contains is behind it.
	var compares strings.Builder
	for i, c := range compare {
		fmt.Fprintf(&compares, " d%d:compare(headRef:%q){status}", i, c.Sha)
	}
	repo, err = mergedQuery(list[0].Repo, owner, name, "defaultBranchRef{"+compares.String()+"}")
	if err != nil {
		return err
	}
	var def map[string]*struct {
		Status string `json:"status"`
	}
	if raw, ok := repo["defaultBranchRef"]; ok {
		_ = json.Unmarshal(raw, &def)
	}
	for i, c := range compare {
		if d := def[fmt.Sprintf("d%d", i)]; d != nil && (d.Status == "BEHIND" || d.Status == "IDENTICAL") {
			merged[commitKey(c)] = true
		}
	}
	return nil
}

func mergedQuery(full, owner, name, fields string) (map[string]json.RawMessage, error) {
	query := "query($o:String!,$n:String!){repository(owner:$o,name:$n){" + fields + "}}"
	out, err := Gh([]string{"api", "graphql", "-f", "query=" + query, "-f", "o=" + owner, "-f", "n=" + name})
	if err != nil {
		return nil, fmt.Errorf("could not check whether %s's commits are merged: %w", full, err)
	}
	var resp struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("could not read whether %s's commits are merged: %v", full, err)
	}
	return resp.Data.Repository, nil
}

// withMerged is r's merged_refs with the commits in merged added.
func withMerged(r Row, merged map[string]bool) (string, bool) {
	known := mergedSet(r)
	var list []string
	for _, c := range ParseCommitRefs(r["merged_refs"]) {
		list = append(list, FormatCommitRef(c.Repo, c.Sha))
	}
	changed := false
	for _, c := range RowCommitRefs(r) {
		k := commitKey(c)
		if merged[k] && !known[k] {
			known[k] = true
			list = append(list, FormatCommitRef(c.Repo, c.Sha))
			changed = true
		}
	}
	return strings.Join(list, ","), changed
}

// RefreshMergedRefs checks the commits of every entry that may still get its
// Worklog's reference, and records the ones now merged. It reports whether any
// entry changed. Errors leave the entries as they were.
func (s *Store) RefreshMergedRefs() (bool, error) {
	rows, err := s.ReadRows()
	if err != nil {
		return false, err
	}
	var refs []CommitRef
	legacy := false
	for _, r := range rows {
		if mergeCheckWanted(r) {
			refs = append(refs, UnmergedRefs(r)...)
			legacy = legacy || legacyMayClear(r)
		}
	}
	if len(refs) == 0 {
		return false, nil
	}
	merged, checkErr := MergedCommits(refs)
	// A legacy tag clears on what is not merged, so only a complete check
	// may clear one; an unreadable repo would look all unmerged.
	if len(merged) == 0 && (!legacy || checkErr != nil) {
		return false, checkErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err = s.readRowsLocked()
	if err != nil {
		return false, err
	}
	changed := false
	for _, r := range rows {
		if mergeCheckWanted(r) && applyMerged(r, merged, checkErr == nil) {
			changed = true
		}
	}
	if !changed {
		return false, checkErr
	}
	return true, s.writeRowsLocked(rows)
}

// CheckRowMerged checks r's commits just before it is pushed and returns r with
// its merged_refs brought up to date (and saved). On an error r comes back as
// it was: the rewrite after the push still refuses a merged commit.
func (s *Store) CheckRowMerged(r Row) Row {
	if !mergeCheckWanted(r) {
		return r
	}
	merged, err := MergedCommits(UnmergedRefs(r))
	next := Row{}
	for k, v := range r {
		next[k] = v
	}
	if !applyMerged(next, merged, err == nil) {
		return r
	}
	if s.UpdateRow(r["id"], Row{"merged_refs": next["merged_refs"], "legacy": next["legacy"]}) != nil {
		return r
	}
	return next
}
