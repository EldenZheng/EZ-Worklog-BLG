package backend

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DailyReviewPush is a proposed update of the day's shared review issue. The
// saved row remains one batch; only the GitHub payload contains the day total.
type DailyReviewPush struct {
	URL             string
	Body            string
	Minutes         int
	ExistingMinutes int
	NeedsCombine    bool
	row             Row
	before          dailyReviewState
}

type dailyReviewState struct {
	URL, Body, ItemID string
	Worklog           WorklogItem
}

const dailyReviewQuery = `query($owner:String!,$repo:String!,$num:Int!){
 repository(owner:$owner,name:$repo){issue(number:$num){url body
  projectItems(first:20){nodes{id
   project{id number fields(first:60){nodes{
    ... on ProjectV2FieldCommon{id name dataType}
    ... on ProjectV2SingleSelectField{options{id name}}
   }}}
   fieldValues(first:50){nodes{
    ... on ProjectV2ItemFieldTextValue{text field{... on ProjectV2FieldCommon{name}}}
    ... on ProjectV2ItemFieldNumberValue{number field{... on ProjectV2FieldCommon{name}}}
    ... on ProjectV2ItemFieldDateValue{date field{... on ProjectV2FieldCommon{name}}}
   }}
  }}
 }}
}`

func readDailyReview(ref string) (dailyReviewState, error) {
	o, r, n, err := SplitIssue(ref)
	if err != nil {
		return dailyReviewState{}, err
	}
	out, err := Gh([]string{"api", "graphql", "-f", "query=" + dailyReviewQuery,
		"-f", "owner=" + o, "-f", "repo=" + r, "-F", fmt.Sprintf("num=%d", n)})
	if err != nil {
		return dailyReviewState{}, err
	}
	var response struct {
		Data struct {
			Repository struct {
				Issue *struct {
					URL, Body    string
					ProjectItems struct {
						Nodes []struct {
							projectItem
							FieldValues struct{ Nodes []PvFieldValue }
						}
					}
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		return dailyReviewState{}, GhErr("Could not read existing code review: %v", err)
	}
	issue := response.Data.Repository.Issue
	if issue == nil {
		return dailyReviewState{}, GhErr("Could not read existing code review %s.", ref)
	}
	state := dailyReviewState{URL: issue.URL, Body: issue.Body}
	var items []projectItem
	for _, item := range issue.ProjectItems.Nodes {
		items = append(items, item.projectItem)
	}
	if chosen := pickProject(items); chosen != nil {
		state.ItemID = chosen.ID
		for _, item := range issue.ProjectItems.Nodes {
			if item.ID == chosen.ID {
				state.Worklog, _ = WorklogFromNode(nil, item.FieldValues.Nodes)
				break
			}
		}
	}
	return state, nil
}

// MergeCodeReviewItems keeps each PR once, making a retry safe even if only
// the description was written before a failed board update. Conflicting time
// for the same PR needs an explicit edit, rather than guessing which to keep.
func MergeCodeReviewItems(lists ...string) ([]CodeReviewItem, error) {
	var merged []CodeReviewItem
	seen := map[string]int{}
	for _, text := range lists {
		if strings.TrimSpace(text) == "" {
			continue
		}
		items, err := ParseCodeReviewItems(text)
		if err != nil {
			return nil, GhErr("Cannot safely combine code reviews: %v. Check the existing description and worklog remarks first.", err)
		}
		for _, item := range items {
			key := strings.ToLower(item.URL)
			if minutes, exists := seen[key]; exists {
				if minutes != item.Minutes {
					return nil, GhErr("%s already has %d minutes, but this list says %d. Edit that PR's existing review time before pushing.", item.URL, minutes, item.Minutes)
				}
				continue
			}
			seen[key] = item.Minutes
			merged = append(merged, item)
		}
	}
	return merged, nil
}

// PrepareDailyReviewPush reads GitHub, including reviews pushed by another
// session, before offering the user a combined total. No GitHub writes here.
func PrepareDailyReviewPush(row Row) (*DailyReviewPush, error) {
	state, err := readDailyReview(row["issue"])
	if err != nil {
		return nil, err
	}
	if state.Worklog.Date != "" && state.Worklog.Date != row["date"] {
		return nil, GhErr("This review is already filed on %s. Use that day's entry or choose another date.", state.Worklog.Date)
	}
	if state.Worklog.Owner != "" && !strings.EqualFold(strings.TrimSpace(state.Worklog.Owner), strings.TrimSpace(row["owner"])) {
		return nil, GhErr("This review belongs to %s. Its time was left unchanged.", state.Worklog.Owner)
	}
	existing, err := MergeCodeReviewItems(state.Body, state.Worklog.Remarks)
	if err != nil {
		return nil, err
	}
	if state.Worklog.Minutes > CodeReviewMinutes(existing) {
		return nil, GhErr("The board has %d review minutes, but its PR lists explain only %d. Check the existing issue before combining.", state.Worklog.Minutes, CodeReviewMinutes(existing))
	}
	items, err := MergeCodeReviewItems(state.Body, state.Worklog.Remarks, row["issue_body"], row["remarks"])
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, GhErr("There are no PR reviews to push.")
	}
	body := CodeReviewBody(items)
	if len(body) > RemarkCap {
		return nil, GhErr("The combined PR list is %d characters; worklog remarks allow %d. Nothing was overwritten.", len(body), RemarkCap)
	}
	return &DailyReviewPush{URL: state.URL, Body: body, Minutes: CodeReviewMinutes(items),
		ExistingMinutes: CodeReviewMinutes(existing),
		NeedsCombine:    state.Worklog.Minutes > 0 || state.Worklog.Remarks != "" || body != strings.TrimSpace(row["remarks"]),
		row:             row, before: state}, nil
}

// PushDailyReview rechecks the approved snapshot before updating both copies
// of the PR list and the board total. It never adds minutes to a stored total.
func PushDailyReview(cfg Config, plan *DailyReviewPush, progress ...func(FetchProgress)) (PushResult, error) {
	current, err := readDailyReview(plan.row["issue"])
	if err != nil {
		return PushResult{}, err
	}
	if current.Body != plan.before.Body || current.ItemID != plan.before.ItemID ||
		current.Worklog.Date != plan.before.Worklog.Date || current.Worklog.Owner != plan.before.Worklog.Owner ||
		current.Worklog.Minutes != plan.before.Worklog.Minutes || current.Worklog.Remarks != plan.before.Worklog.Remarks {
		return PushResult{}, GhErr("The code review changed on GitHub while you were choosing. Push again to check the new combined total.")
	}
	if _, err := Gh([]string{"issue", "edit", plan.URL, "--body", plan.Body}); err != nil {
		return PushResult{}, err
	}
	r := plan.row
	return PushEntry(cfg, r["issue"], r["date"], r["owner"], plan.Minutes,
		plan.Body, plan.Body, "issue", r["issue_url"], "", progress...)
}
