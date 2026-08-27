package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ---- AI compaction (Anthropic) ----

const aiPromptTmpl = "Rewrite this work log so it fits within %d characters.\n" +
	"Rules: keep every distinct piece of work as its own '- ' bullet; never merge two " +
	"different tasks into one bullet; never invent work that isn't listed; shorten wording " +
	"and drop commit hashes if needed. Reply with the rewritten log only, no preamble.\n\n%s"

func aiCompact(cfg Config, text string) (string, error) {
	key := strings.TrimSpace(cfg.AnthropicAPIKey)
	if key == "" {
		return "", ghErr("Add an Anthropic API key in Settings to use AI compaction.")
	}
	model := cfg.AnthropicModel
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	payload := map[string]any{
		"model":      model,
		"max_tokens": 1200,
		"messages": []map[string]string{
			{"role": "user", "content": fmt.Sprintf(aiPromptTmpl, remarkCap, text)},
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", ghErr("Anthropic API error: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", ghErr("Anthropic API error: %s", strings.TrimSpace(string(raw)))
	}
	var data struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", ghErr("Anthropic API error: %v", err)
	}
	var sb strings.Builder
	for _, b := range data.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", ghErr("The model returned nothing.")
	}
	if len(out) > remarkCap {
		out = compactLocally(strings.Split(out, "\n"))
	}
	return out, nil
}

// ---- currency ----

func fetchRate(base, target string) (float64, error) {
	url := "https://open.er-api.com/v6/latest/" + base
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, ghErr("Could not reach the rate service: %v", err)
	}
	defer resp.Body.Close()
	var data struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, ghErr("Could not reach the rate service: %v", err)
	}
	rate, ok := data.Rates[target]
	if !ok || rate == 0 {
		return 0, ghErr("No rate for %s->%s.", base, target)
	}
	return rate, nil
}

// ---- reporting ----

type dayEntry struct {
	Date    string
	Minutes int
}

// Report mirrors the /api/report payload.
type Report struct {
	Currency        string
	DisplayCurrency string
	FxRate          float64
	FxUpdated       string
	BaseSalary      float64
	DailyRate       float64
	DaysLogged      int
	DaysComplete    int
	TotalMin        int
	PayableDays     float64
	OvertimeMin     int
	Receivable      float64
	Totals          map[string]int
	Incomplete      []dayEntry
	Over            []dayEntry
	Unpushed        int
}

func dayTotals(rows []Row) map[string]int {
	t := map[string]int{}
	for _, r := range rows {
		t[r["date"]] += r.Minutes()
	}
	return t
}

func monthReport(cfg Config, month string, allRows []Row) Report {
	var monthRows []Row
	for _, r := range allRows {
		if strings.HasPrefix(r["date"], month) {
			monthRows = append(monthRows, r)
		}
	}
	totals := dayTotals(monthRows)
	rate := cfg.BaseSalary / divisor
	payable := 0
	totalMin := 0
	daysLogged := 0
	daysComplete := 0
	overtime := 0
	var incomplete, over []dayEntry
	for d, m := range totals {
		p := m
		if p > target {
			p = target
		}
		if p < 0 {
			p = 0
		}
		payable += p
		totalMin += m
		if m > 0 {
			daysLogged++
		}
		if m >= target {
			daysComplete++
		}
		if m > target {
			overtime += m - target
			over = append(over, dayEntry{d, m})
		}
		if m > 0 && m < target {
			incomplete = append(incomplete, dayEntry{d, m})
		}
	}
	sort.Slice(incomplete, func(i, j int) bool { return incomplete[i].Date < incomplete[j].Date })
	sort.Slice(over, func(i, j int) bool { return over[i].Date < over[j].Date })

	unpushed := 0
	for _, r := range monthRows {
		if r["issue"] != "" && r["pushed_at"] == "" {
			unpushed++
		}
	}
	payableDays := float64(payable) / target
	return Report{
		Currency:        cfg.Currency,
		DisplayCurrency: cfg.DisplayCurrency,
		FxRate:          cfg.FxRate,
		FxUpdated:       cfg.FxUpdated,
		BaseSalary:      cfg.BaseSalary,
		DailyRate:       rate,
		DaysLogged:      daysLogged,
		DaysComplete:    daysComplete,
		TotalMin:        totalMin,
		PayableDays:     round2(payableDays),
		OvertimeMin:     overtime,
		Receivable:      round2(payableDays * rate),
		Totals:          totals,
		Incomplete:      incomplete,
		Over:            over,
		Unpushed:        unpushed,
	}
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// workingDaysProgress counts Mon–Fri days in a payroll period and how many of
// them are already behind you.
//
// The total is the calendar's, not the pay divisor's: a 21st-to-20th window
// holds 21, 22 or 23 weekdays depending on where the weekends land, while the
// salary always divides by 21. A period running over is normal and is exactly
// what this is meant to show.
//
// Today counts as remaining, not as gone — there are still hours to log in it.
// Before the period opens nothing has gone; after it closes everything has, so
// a finished period reads n/n with zero left.
func workingDaysProgress(fromDate, toDate, asOf string) (gone, total int) {
	start, err1 := time.Parse("2006-01-02", fromDate)
	end, err2 := time.Parse("2006-01-02", toDate)
	if err1 != nil || err2 != nil || end.Before(start) {
		return 0, 0
	}
	now, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		now = end.AddDate(0, 0, 1) // unreadable date: treat the period as closed
	}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		total++
		if d.Before(now) {
			gone++
		}
	}
	return gone, total
}

// totalsFromItems sums project worklog minutes per date for one month.
func totalsFromItems(items []WorklogItem, month string) map[string]int {
	t := map[string]int{}
	for _, it := range items {
		if strings.HasPrefix(it.Date, month) {
			t[it.Date] += it.Minutes
		}
	}
	return t
}

// reportFromTotals runs the pay math on a pre-aggregated day→minutes map.
func reportFromTotals(cfg Config, totals map[string]int) Report {
	rate := cfg.BaseSalary / divisor
	payable, totalMin, daysLogged, daysComplete, overtime := 0, 0, 0, 0, 0
	var incomplete, over []dayEntry
	for d, m := range totals {
		p := m
		if p > target {
			p = target
		}
		if p < 0 {
			p = 0
		}
		payable += p
		totalMin += m
		if m > 0 {
			daysLogged++
		}
		if m >= target {
			daysComplete++
		}
		if m > target {
			overtime += m - target
			over = append(over, dayEntry{d, m})
		}
		if m > 0 && m < target {
			incomplete = append(incomplete, dayEntry{d, m})
		}
	}
	sort.Slice(incomplete, func(i, j int) bool { return incomplete[i].Date < incomplete[j].Date })
	sort.Slice(over, func(i, j int) bool { return over[i].Date < over[j].Date })
	payableDays := float64(payable) / target
	return Report{
		Currency: cfg.Currency, DisplayCurrency: cfg.DisplayCurrency,
		FxRate: cfg.FxRate, FxUpdated: cfg.FxUpdated,
		BaseSalary: cfg.BaseSalary, DailyRate: rate,
		DaysLogged: daysLogged, DaysComplete: daysComplete, TotalMin: totalMin,
		PayableDays: round2(payableDays), OvertimeMin: overtime,
		Receivable: round2(payableDays * rate),
		Totals:     totals, Incomplete: incomplete, Over: over, Unpushed: 0,
	}
}
