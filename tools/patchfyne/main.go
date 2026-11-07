// Command patchfyne prepares the private copy of Fyne the app is built against,
// on any platform. build.ps1 and build.sh both run it, so a Windows and a Mac
// build carry the same cursors, button colours and mouse wheel.
//
// It never touches the shared module cache or the project's own go.mod: the copy
// and a go.mod pointing at it go in the stage folder, and the build is run with
// -modfile <stage>/worklog.mod.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// pinnedFyne is the version these patches were written against.
const pinnedFyne = "v2.8.0"

type control struct {
	file, typeName, disabled string
	existing                 bool // Fyne already has a Cursor method to replace
}

// The hand-cursor policy: a pointer over anything that can be pressed, and the
// plain arrow once it is disabled.
var controls = []control{
	{"widget/button.go", "Button", "c.Disabled()", true},
	{"widget/check.go", "Check", "c.Disabled()", false},
	{"widget/select.go", "Select", "c.Disabled()", false},
	{"widget/radio_item.go", "radioItem", "c.Disabled()", false},
	{"widget/menu_item.go", "menuItem", "c.Item.Disabled || c.Item.IsSeparator", false},
	{"container/tabs.go", "tabButton", "c.Disabled()", false},
}

// replacement is one exact change to a Fyne source file. If the text is not
// found the version has moved on, and the patch needs a person to look at it.
type replacement struct{ original, replaced, review string }

var buttonChanges = []replacement{
	// Standard gold buttons need dark labels and icons; primary blue actions and
	// low-importance icon controls keep their own colours.
	{"default:\n\t\t\tbackground = theme.ColorNameButton",
		"default:\n\t\t\tforeground = theme.ColorNameForegroundOnWarning\n\t\t\tbackground = theme.ColorNameButton",
		"the standard button foreground policy"},
	{"r.button.Importance != MediumImportance && r.button.Importance != LowImportance",
		"r.button.Importance != LowImportance",
		"the button icon foreground policy"},
	// The press flash takes the button's rounded corners; stock Fyne draws it
	// square over the rounded hover background.
	{"tapBG := canvas.NewRectangle(color.Transparent)",
		"tapBG := canvas.NewRectangle(color.Transparent)\n\ttapBG.CornerRadius = background.CornerRadius",
		"the button press flash"},
	{"bg.CornerRadius = th.Size(theme.SizeNameButtonRadius)\n\t\tbg.Refresh()",
		"bg.CornerRadius = th.Size(theme.SizeNameButtonRadius)\n\t\tbg.Refresh()\n\t\tr.tapBG.CornerRadius = bg.CornerRadius",
		"the button press flash theme update"},
}

func main() {
	stage := flag.String("stage", filepath.Join(".build", "cursors"), "folder for the patched copy and its go.mod")
	flag.Parse()
	if err := run(*stage); err != nil {
		fmt.Fprintln(os.Stderr, "patchfyne:", err)
		os.Exit(1)
	}
}

func run(stage string) error {
	stage, err := filepath.Abs(stage)
	if err != nil {
		return err
	}
	// Download, not list: on a fresh machine (a GitHub runner) nothing is in the
	// module cache yet, and go list leaves Dir empty rather than fetching it.
	out, err := exec.Command("go", "mod", "download", "-json", "fyne.io/fyne/v2").Output()
	if err != nil {
		return fmt.Errorf("cannot download Fyne: %w", err)
	}
	var module struct{ Version, Dir string }
	if err := json.Unmarshal(out, &module); err != nil {
		return fmt.Errorf("cannot read the Fyne module: %w", err)
	}
	if module.Dir == "" {
		return fmt.Errorf("the Fyne module has no folder in the module cache")
	}
	if module.Version != pinnedFyne {
		return fmt.Errorf("review the patches for Fyne %s, they were written for %s", module.Version, pinnedFyne)
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	dependency := filepath.Join(stage, "fyne-"+pinnedFyne)
	if err := copyModuleOnce(module.Dir, dependency); err != nil {
		return err
	}
	for _, c := range controls {
		if err := patchControl(module.Dir, dependency, c); err != nil {
			return err
		}
	}
	if err := patchWheel(module.Dir, dependency); err != nil {
		return err
	}
	if err := copyPatchFiles("_fynepatch", dependency); err != nil {
		return err
	}
	return writeBuildMod(stage, dependency)
}

// readSource reads a file from the module cache, with LF line endings whatever
// the checkout used, so the exact-text changes below match on every platform.
func readSource(dir, file string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n"), nil
}

func writePatched(dependency, file, code string) error {
	return writeFile(filepath.Join(dependency, filepath.FromSlash(file)), []byte(code))
}

// writeFile replaces path. Files copied out of the module cache can be read-only.
func writeFile(path string, data []byte) error {
	_ = os.Chmod(path, 0o644)
	return os.WriteFile(path, data, 0o644)
}

func patchControl(source, dependency string, c control) error {
	path := filepath.Join(source, filepath.FromSlash(c.file))
	code, err := readSource(source, c.file)
	if err != nil {
		return err
	}
	method := fmt.Sprintf("func (c *%s) Cursor() desktop.Cursor {\n    if %s {\n        return desktop.DefaultCursor\n    }\n    return desktop.PointerCursor\n}",
		c.typeName, c.disabled)
	if c.existing {
		original := "func (b *Button) Cursor() desktop.Cursor {\n\treturn desktop.DefaultCursor\n}"
		if !strings.Contains(code, original) {
			return fmt.Errorf("unexpected cursor implementation in %s", path)
		}
		code = strings.Replace(code, original, method, 1)
	} else {
		pattern := regexp.MustCompile(`func \([^)]*\*` + c.typeName + `\) Cursor\(`)
		if pattern.MatchString(code) {
			return fmt.Errorf("Fyne now supplies a cursor for %s; review the local policy", c.typeName)
		}
		code += "\n" + method + "\n"
	}
	if c.typeName == "Button" {
		for _, change := range buttonChanges {
			if !strings.Contains(code, change.original) {
				return fmt.Errorf("review %s for this Fyne version", change.review)
			}
			code = strings.ReplaceAll(code, change.original, change.replaced)
		}
	}
	return writePatched(dependency, c.file, code)
}

// patchWheel renames the window's own wheel handler out of the way, so the one in
// _fynepatch replaces it.
func patchWheel(source, dependency string) error {
	const file = "internal/driver/glfw/window.go"
	code, err := readSource(source, file)
	if err != nil {
		return err
	}
	original := "func (w *window) processMouseScrolled(xoff float64, yoff float64) {"
	if !strings.Contains(code, original) {
		return fmt.Errorf("review the mouse wheel patch for this Fyne version")
	}
	code = strings.ReplaceAll(code, original, "func (w *window) fyneProcessMouseScrolled(xoff float64, yoff float64) {")
	return writePatched(dependency, file, code)
}

// copyPatchFiles adds the whole files in root to the copy, at the same paths.
func copyPatchFiles(root, dependency string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(dependency, relative), data)
	})
}

// copyModuleOnce copies the module cache's Fyne, unless a copy is already there.
// It builds under another name first, so a copy cut short is not taken for a
// finished one next time.
func copyModuleOnce(source, dependency string) error {
	if _, err := os.Stat(dependency); err == nil {
		return nil
	}
	partial := dependency + ".partial"
	if err := os.RemoveAll(partial); err != nil {
		return err
	}
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(partial, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()|0o200)
	})
	if err != nil {
		return err
	}
	return os.Rename(partial, dependency)
}

// writeBuildMod writes the go.mod and go.sum the build runs with: the project's
// own, with Fyne pointed at the copy.
func writeBuildMod(stage, dependency string) error {
	buildMod := filepath.Join(stage, "worklog.mod")
	for from, to := range map[string]string{"go.mod": buildMod, "go.sum": filepath.Join(stage, "worklog.sum")} {
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := writeFile(to, data); err != nil {
			return err
		}
	}
	edit := exec.Command("go", "mod", "edit", "-modfile", buildMod, "-replace=fyne.io/fyne/v2="+dependency)
	if out, err := edit.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot configure the private Fyne build: %w: %s", err, out)
	}
	return nil
}
