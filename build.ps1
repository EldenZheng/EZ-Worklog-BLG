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
    # A private build copy keeps the cursor policy local to this application. The
    # same tool prepares it for build.sh, so every platform gets the same patches.
    go run ./tools/patchfyne
    if ($LASTEXITCODE -ne 0) { throw 'Cannot prepare the private Fyne build' }
    $buildMod = Join-Path $PSScriptRoot '.build/cursors/worklog.mod'
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
