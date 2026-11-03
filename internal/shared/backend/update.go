package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Updates come from GitHub Releases on the app's own repository: build.ps1
// -Release builds Worklog.exe and attaches it to a release tagged vX.Y.Z, and
// a running copy compares that tag with the version it was built as. Nobody
// needs git, Go or a compiler to update; only gh, which the app already uses.

// UpdateRepo is where releases are published.
const UpdateRepo = "EldenZheng/EZ-Worklog-BLG"

// ReleaseAsset is the file attached to every release.
const ReleaseAsset = "Worklog.exe"

// Version is the version this binary was built as. build.ps1 sets it with
// -ldflags -X: the release tag for a release build, `git describe` output such
// as v1.2.0-3-gabc1234-dirty for any other build. A plain `go build` leaves
// "dev".
var Version = "dev"

// Release is one published release.
type Release struct {
	Tag       string    `json:"tagName"`
	Published time.Time `json:"publishedAt"`
	URL       string    `json:"url"`
}

// LatestRelease asks GitHub for the newest published release. A variable so
// tests can stand in for gh.
var LatestRelease = func() (Release, error) {
	out, err := Gh([]string{"release", "view", "--repo", UpdateRepo, "--json", "tagName,publishedAt,url"})
	if err != nil {
		if strings.Contains(err.Error(), "release not found") {
			return Release{}, GhErr("No release has been published yet.")
		}
		return Release{}, err
	}
	var r Release
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil {
		return Release{}, GhErr("could not read the latest release: %v", err)
	}
	return r, nil
}

var versionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// parseVersion reads the vX.Y.Z at the front of a tag or a describe string.
func parseVersion(s string) ([3]int, bool) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

// ValidReleaseTag reports whether tag is a plain vX.Y.Z, the only form a
// release is published under.
func ValidReleaseTag(tag string) bool {
	return regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(tag)
}

// NewerRelease reports whether the release tagged latest is newer than the
// build current. A build with no version in it ("dev", or a bare commit id
// from a clone with no tags yet) counts as older than any release, so it is
// always offered the published one. A build a few commits past v1.2.0 is not
// offered v1.2.0 again.
func NewerRelease(latest, current string) bool {
	l, ok := parseVersion(latest)
	if !ok {
		return false
	}
	c, ok := parseVersion(current)
	if !ok {
		return true
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// DownloadRelease fetches tag's Worklog.exe into a new temporary folder and
// returns its path. A variable so tests can stand in for gh.
var DownloadRelease = func(tag string) (string, error) {
	dir, err := os.MkdirTemp("", "worklog-update-")
	if err != nil {
		return "", err
	}
	if _, err := Gh([]string{"release", "download", tag, "--repo", UpdateRepo,
		"--pattern", ReleaseAsset, "--dir", dir, "--clobber"}); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	path := filepath.Join(dir, ReleaseAsset)
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		os.RemoveAll(dir)
		return "", GhErr("Release %s has no %s attached.", tag, ReleaseAsset)
	}
	return path, nil
}

// previousSuffix marks the binary an update replaced. Windows will not let a
// running exe be overwritten or deleted, but it will let it be renamed: the
// running copy steps aside under this name, the new one takes its place, and
// the next start deletes the old one.
const previousSuffix = ".previous"

// InstallUpdate puts the exe at downloaded in place of exe, the binary that is
// running now. On any failure the running binary is put back, so a failed
// update leaves the app exactly as it was.
func InstallUpdate(downloaded, exe string) error {
	previous := exe + previousSuffix
	_ = os.Remove(previous) // a leftover from an update that never restarted
	if err := os.Rename(exe, previous); err != nil {
		return fmt.Errorf("could not move the running app aside: %w", err)
	}
	if err := copyFile(downloaded, exe); err != nil {
		_ = os.Remove(exe)
		_ = os.Rename(previous, exe)
		return fmt.Errorf("could not install the update: %w", err)
	}
	_ = os.RemoveAll(filepath.Dir(downloaded))
	return nil
}

// RemovePreviousBinary deletes the binary the last update replaced. Run at
// start-up, when nothing is running from it any more.
func RemovePreviousBinary(exe string) {
	_ = os.Remove(exe + previousSuffix)
}

// Relaunch starts exe again, from its own folder, so the updated app opens as
// this one closes. A variable so tests do not start anything.
var Relaunch = func(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	return cmd.Start()
}

// ExecutablePath is the binary that is running, with links resolved: the file
// an update replaces. A variable so tests can point it at a stand-in file.
var ExecutablePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
