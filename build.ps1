param(
    [switch]$Test,
    [string]$Output = '..\Worklog.exe',
    # Publish the build as a GitHub release with this tag, e.g. v1.4.0. The
    # copies already out there offer it as an update the next time they start.
    [string]$Release = ''
)
$ErrorActionPreference = 'Stop'
$UpdateRepo = 'EldenZheng/EZ-Worklog-BLG'
Push-Location $PSScriptRoot
try {
    if ($Release) {
        # A release must be exactly the code on GitHub, tested, under a new
        # vX.Y.Z tag. Each check stops before anything is built or published.
        if ($Release -notmatch '^v\d+\.\d+\.\d+$') { throw "Release must look like v1.2.3, got '$Release'." }
        if ((Split-Path $Output -Leaf) -ne 'Worklog.exe') { throw 'A release must be built as Worklog.exe; that is the name the app downloads.' }
        if (git status --porcelain) { throw 'Commit everything before releasing: the release must be the code on GitHub.' }
        git fetch origin --tags --quiet
        if ($LASTEXITCODE -ne 0) { throw 'Cannot reach GitHub to check the branch.' }
        $ahead = git rev-list --count '@{u}..HEAD'
        if ($ahead -ne '0') { throw "Push your $ahead commit(s) first: the release must be the code on GitHub." }
        $behind = git rev-list --count 'HEAD..@{u}'
        if ($behind -ne '0') { throw "Pull the $behind commit(s) on GitHub first." }
        if (git tag --list $Release) { throw "$Release is already released; pick the next version." }
        $Test = $true
        $version = $Release
    } else {
        # Any other build says where it came from: the last release tag, the
        # commits since it, and -dirty when there are uncommitted changes.
        $version = git describe --tags --always --dirty 2>$null
        if ($LASTEXITCODE -ne 0 -or -not $version) { $version = 'dev' }
    }
    # A private build copy keeps the cursor policy local to this application.
    # It includes Fyne-created dialog, dropdown and tab controls without
    # changing the shared module cache or replacing their event handlers.
    $module = (go list -m -json fyne.io/fyne/v2 | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0) { throw 'Cannot locate Fyne' }
    if ($module.Version -ne 'v2.8.0') { throw 'Review the cursor policy for the new Fyne version before building.' }
    $stage = Join-Path $PSScriptRoot '.build/cursors'
    New-Item -ItemType Directory -Force -Path $stage | Out-Null
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    $dependency = Join-Path $stage 'fyne-v2.8.0'
    if (-not (Test-Path -LiteralPath $dependency)) {
        Copy-Item -LiteralPath $module.Dir -Destination $dependency -Recurse
    }
    $controls = @(
        @{ File = 'widget/button.go'; Type = 'Button'; Disabled = 'c.Disabled()'; Existing = $true },
        @{ File = 'widget/check.go'; Type = 'Check'; Disabled = 'c.Disabled()' },
        @{ File = 'widget/select.go'; Type = 'Select'; Disabled = 'c.Disabled()' },
        @{ File = 'widget/radio_item.go'; Type = 'radioItem'; Disabled = 'c.Disabled()' },
        @{ File = 'widget/menu_item.go'; Type = 'menuItem'; Disabled = 'c.Item.Disabled || c.Item.IsSeparator' },
        @{ File = 'container/tabs.go'; Type = 'tabButton'; Disabled = 'c.Disabled()' }
    )
    foreach ($control in $controls) {
        $source = Join-Path $module.Dir $control.File
        $code = [System.IO.File]::ReadAllText($source).Replace("`r`n", "`n")
        $method = @"
func (c *$($control.Type)) Cursor() desktop.Cursor {
    if $($control.Disabled) {
        return desktop.DefaultCursor
    }
    return desktop.PointerCursor
}
"@
        if ($control.Existing) {
            $original = "func (b *Button) Cursor() desktop.Cursor {`n`treturn desktop.DefaultCursor`n}"
            if (-not $code.Contains($original)) { throw "Unexpected cursor implementation in $source" }
            $code = $code.Replace($original, $method)
        } else {
            if ($code -match ('func \([^)]*\*' + $control.Type + '\) Cursor\(')) {
                throw "Fyne now supplies a cursor for $($control.Type); review the local policy."
            }
            $code += "`n" + $method + "`n"
        }
        if ($control.Type -eq 'Button') {
            # Standard gold buttons need dark labels and icons; primary blue
            # actions and low-importance icon controls keep their own colours.
            $original = "default:`n`t`t`tbackground = theme.ColorNameButton"
            $replacement = "default:`n`t`t`tforeground = theme.ColorNameForegroundOnWarning`n`t`t`tbackground = theme.ColorNameButton"
            if (-not $code.Contains($original)) { throw 'Review the standard button foreground policy for this Fyne version.' }
            $code = $code.Replace($original, $replacement)
            $original = 'r.button.Importance != MediumImportance && r.button.Importance != LowImportance'
            if (-not $code.Contains($original)) { throw 'Review the button icon foreground policy for this Fyne version.' }
            $code = $code.Replace($original, 'r.button.Importance != LowImportance')
            # The press flash takes the button's rounded corners; stock Fyne
            # draws it square over the rounded hover background.
            $original = 'tapBG := canvas.NewRectangle(color.Transparent)'
            if (-not $code.Contains($original)) { throw 'Review the button press flash for this Fyne version.' }
            $code = $code.Replace($original, "$original`n`ttapBG.CornerRadius = background.CornerRadius")
            $original = "bg.CornerRadius = th.Size(theme.SizeNameButtonRadius)`n`t`tbg.Refresh()"
            if (-not $code.Contains($original)) { throw 'Review the button press flash theme update for this Fyne version.' }
            $code = $code.Replace($original, "$original`n`t`tr.tapBG.CornerRadius = bg.CornerRadius")
        }
        $patched = Join-Path $dependency $control.File
        (Get-Item -LiteralPath $patched).IsReadOnly = $false
        [System.IO.File]::WriteAllText($patched, $code, $utf8)
    }
    # Mouse wheel: _fynepatch holds whole files added to the copy (see the
    # comments in them). The window's own wheel handler is renamed out of the
    # way so the patched one replaces it.
    $source = Join-Path $module.Dir 'internal/driver/glfw/window.go'
    $code = [System.IO.File]::ReadAllText($source).Replace("`r`n", "`n")
    $original = 'func (w *window) processMouseScrolled(xoff float64, yoff float64) {'
    if (-not $code.Contains($original)) { throw 'Review the mouse wheel patch for this Fyne version.' }
    $code = $code.Replace($original, 'func (w *window) fyneProcessMouseScrolled(xoff float64, yoff float64) {')
    $patched = Join-Path $dependency 'internal/driver/glfw/window.go'
    (Get-Item -LiteralPath $patched).IsReadOnly = $false
    [System.IO.File]::WriteAllText($patched, $code, $utf8)
    $patchRoot = Join-Path $PSScriptRoot '_fynepatch'
    foreach ($file in Get-ChildItem -LiteralPath $patchRoot -Recurse -File -Filter '*.go') {
        $relative = $file.FullName.Substring($patchRoot.Length + 1)
        $target = Join-Path $dependency $relative
        if (Test-Path -LiteralPath $target) { (Get-Item -LiteralPath $target).IsReadOnly = $false }
        Copy-Item -LiteralPath $file.FullName -Destination $target -Force
    }
    $buildMod = Join-Path $stage 'worklog.mod'
    Copy-Item -LiteralPath 'go.mod' -Destination $buildMod -Force
    Copy-Item -LiteralPath 'go.sum' -Destination (Join-Path $stage 'worklog.sum') -Force
    go mod edit -modfile $buildMod "-replace=fyne.io/fyne/v2=$dependency"
    if ($LASTEXITCODE -ne 0) { throw 'Cannot configure the private Fyne build' }
    if ($Test) {
        go test -modfile $buildMod -tags cursors ./...
        if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
    }
    # Linking over the old exe in place left it cut short (1.4 MB of 27) and
    # the app would not open, so start from no file.
    if (Test-Path -LiteralPath $Output) { Remove-Item -LiteralPath $Output -Force }
    go build -modfile $buildMod -ldflags "-H windowsgui -s -w -X worklog/internal/shared/backend.Version=$version" -o $Output .
    if ($LASTEXITCODE -ne 0) { throw 'Build failed' }
    if ($Release) {
        $exe = (Resolve-Path -LiteralPath $Output).Path
        gh release create $Release $exe --repo $UpdateRepo --target (git rev-parse HEAD) --title $Release --generate-notes
        if ($LASTEXITCODE -ne 0) { throw 'Publishing the release failed; nothing was released.' }
        git fetch origin --tags --quiet
        Write-Host "Released $Release. Running copies offer it the next time they start."
    }
} finally {
    Pop-Location
}
