package main

import (
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	target    = 480  // payable minutes per day
	divisor   = 21   // base salary covers this many working days
	remarkCap = 1024 // GitHub project text field cutoff
)

// fieldOrder is the exact CSV column order, matching the Python version.
// parent_repo and parent_title are only filled for an "independent" entry: the
// issue it belongs under does not exist yet, and is created on the first push
// rather than when the entry is saved. Nothing reaches GitHub until you push,
// which is the rest of the app's bargain too.
var fieldOrder = []string{
	"id", "date", "minutes", "type", "description", "refs", "issue",
	"owner", "remarks", "mode", "issue_url", "item_id", "pushed_at", "logged_at",
	"parent_repo", "parent_title",
}

// Config mirrors config.json so data carries over from the Python app.
type Config struct {
	GithubAuthor      string   `json:"github_author"`
	Repos             []string `json:"repos"`
	BaseSalary        float64  `json:"base_salary_per_21_days"`
	Currency          string   `json:"currency"`
	DisplayCurrency   string   `json:"display_currency"`
	FxRate            float64  `json:"fx_rate"`
	FxUpdated         string   `json:"fx_updated"`
	LookbackDays      int      `json:"lookback_days"`
	WorklogOwner      string   `json:"worklog_owner"`
	DefaultMode       string   `json:"default_mode"`
	AnthropicAPIKey   string   `json:"anthropic_api_key"`
	AnthropicModel    string   `json:"anthropic_model"`
	ProjectURL        string   `json:"project_url"`
	ExtraProjectURLs  []string `json:"extra_project_urls"`
	// DisplayName is the name a meeting issue is titled with — "Elden" in
	// "Elden Meeting & ad hocs: 2026-08-25". Blank means derive it from the
	// worklog owner; see displayName.
	DisplayName string `json:"display_name"`
}

func defaultConfig() Config {
	return Config{
		// No salary is assumed. It used to default to one person's figure, which
		// meant a fresh install opened the Report on a full month of money that
		// was quietly wrong for whoever was reading it — and wrong in a way that
		// looks right, because every other number on the tab is real. Zero reads
		// as "not set yet", which is what it is.
		BaseSalary:     0,
		Currency:       "RM",
		DisplayCurrency: "USD",
		LookbackDays:   7,
		DefaultMode:    "subissue",
		AnthropicModel: "claude-sonnet-4-6",
	}
}

// Row is one worklog line. All values are strings for CSV fidelity.
type Row map[string]string

func (r Row) Minutes() int {
	n, _ := strconv.Atoi(strings.TrimSpace(r["minutes"]))
	return n
}

// Store owns the on-disk files and guards them with a mutex.
type Store struct {
	dir     string
	config  string
	logf    string
	ignoref string
	mu      sync.Mutex
}

func newStore(dir string) *Store {
	return &Store{
		dir:     dir,
		config:  filepath.Join(dir, "config.json"),
		logf:    filepath.Join(dir, "worklog.csv"),
		ignoref: filepath.Join(dir, "ignored.json"),
	}
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- config ----

// LoadConfig returns (config, exists). When the file is missing, exists is false
// and a default config is returned.
func (s *Store) LoadConfig() (Config, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := defaultConfig()
	data, err := os.ReadFile(s.config)
	if err != nil {
		return cfg, false
	}
	// Unmarshal over defaults so missing keys keep their fallback.
	_ = json.Unmarshal(data, &cfg)
	if cfg.AnthropicModel == "" {
		cfg.AnthropicModel = "claude-sonnet-4-6"
	}
	return cfg, true
}

func (s *Store) SaveConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.config, data, 0o644)
}

// ---- csv rows ----

func (s *Store) ReadRows() ([]Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRowsLocked()
}

func (s *Store) readRowsLocked() ([]Row, error) {
	f, err := os.Open(s.logf)
	if err != nil {
		if os.IsNotExist(err) {
			return []Row{}, nil
		}
		return nil, err
	}
	defer f.Close()
	rd := csv.NewReader(f)
	rd.FieldsPerRecord = -1
	records, err := rd.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return []Row{}, nil
	}
	header := records[0]
	out := make([]Row, 0, len(records)-1)
	for _, rec := range records[1:] {
		r := Row{}
		for _, k := range fieldOrder {
			r[k] = ""
		}
		for i, k := range header {
			if i < len(rec) {
				r[k] = rec[i]
			}
		}
		if r["id"] == "" {
			r["id"] = newID()
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) writeRowsLocked(rows []Row) error {
	f, err := os.Create(s.logf)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(fieldOrder); err != nil {
		return err
	}
	for _, r := range rows {
		rec := make([]string, len(fieldOrder))
		for i, k := range fieldOrder {
			rec[i] = r[k]
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func (s *Store) WriteRows(rows []Row) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeRowsLocked(rows)
}

// AppendRows fills in id + logged_at and returns the created rows.
func (s *Store) AppendRows(newRows []Row) ([]Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.readRowsLocked()
	if err != nil {
		return nil, err
	}
	made := make([]Row, 0, len(newRows))
	now := time.Now().Format("2006-01-02T15:04:05")
	for _, n := range newRows {
		row := Row{}
		for _, k := range fieldOrder {
			row[k] = ""
		}
		for k, v := range n {
			row[k] = v
		}
		row["id"] = newID()
		row["logged_at"] = now
		rows = append(rows, row)
		made = append(made, row)
	}
	if err := s.writeRowsLocked(rows); err != nil {
		return nil, err
	}
	return made, nil
}

func (s *Store) UpdateRow(id string, patch Row) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.readRowsLocked()
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r["id"] == id {
			for k, v := range patch {
				r[k] = v
			}
		}
	}
	return s.writeRowsLocked(rows)
}

func (s *Store) DeleteRow(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.readRowsLocked()
	if err != nil {
		return err
	}
	kept := make([]Row, 0, len(rows))
	for _, r := range rows {
		if r["id"] != id {
			kept = append(kept, r)
		}
	}
	return s.writeRowsLocked(kept)
}

// ---- ignored commits ----

// The pending list only knows two states: a commit is logged, or it is waiting.
// Some commits are neither — a revert, a formatting sweep, work already covered
// by another entry — and the only way to clear them was to log time against
// them. Their shas live in their own file rather than in the CSV, because there
// is no worklog row to hang them off and no minutes to invent for one.

// IgnoredShas is the set of commit shas deliberately kept out of the list.
func (s *Store) IgnoredShas() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readIgnoredLocked()
}

func (s *Store) readIgnoredLocked() (map[string]bool, error) {
	set := map[string]bool{}
	data, err := os.ReadFile(s.ignoref)
	if err != nil {
		if os.IsNotExist(err) {
			return set, nil // nothing ignored yet is not a failure
		}
		return nil, err
	}
	var shas []string
	if err := json.Unmarshal(data, &shas); err != nil {
		return nil, err
	}
	for _, sha := range shas {
		if sha = strings.TrimSpace(sha); sha != "" {
			set[sha] = true
		}
	}
	return set, nil
}

// SetIgnored adds a commit to the ignore set, or takes it back out.
func (s *Store) SetIgnored(sha string, ignored bool) error {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	set, err := s.readIgnoredLocked()
	if err != nil {
		return err
	}
	if set[sha] == ignored {
		return nil
	}
	if ignored {
		set[sha] = true
	} else {
		delete(set, sha)
	}
	shas := make([]string, 0, len(set))
	for k := range set {
		shas = append(shas, k)
	}
	sort.Strings(shas) // a stable file, so the same set writes the same bytes
	data, err := json.MarshalIndent(shas, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.ignoref, data, 0o644)
}

// LoggedShas is the set of commit shas already recorded (from the refs column).
func (s *Store) LoggedShas() (map[string]bool, error) {
	rows, err := s.ReadRows()
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, r := range rows {
		for _, x := range strings.Split(r["refs"], ",") {
			x = strings.TrimSpace(x)
			if x != "" {
				set[x] = true
			}
		}
	}
	return set, nil
}
