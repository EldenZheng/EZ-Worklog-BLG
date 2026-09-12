param(
    [switch]$Test,
    [string]$Output = 'Worklog.exe'
)
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
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
        $patched = Join-Path $dependency $control.File
        (Get-Item -LiteralPath $patched).IsReadOnly = $false
        [System.IO.File]::WriteAllText($patched, $code, $utf8)
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
    go build -modfile $buildMod -ldflags '-H windowsgui -s -w' -o $Output .
    if ($LASTEXITCODE -ne 0) { throw 'Build failed' }
} finally {
    Pop-Location
}
