# local-server installer for Windows: install, upgrade and uninstall (PLT-F-043; req_001 D-14, D-23,
# D-30, D-49, D-62, D-101, D-149, D-154, D-156). It mirrors scripts/install.sh.
#
#   irm https://github.com/inCoderKaka/local-server/releases/latest/download/install.ps1 | iex
#
# Windows PowerShell 5.1 and PowerShell 7 both run it. It is meant for `irm | iex`, where no param()
# block can take arguments, so the choices come from environment variables:
#
#   $env:LOCAL_SERVER_ACTION = 'upgrade'          # install (default), upgrade or uninstall
#   $env:LOCAL_SERVER_VERSION = 'v0.1.0-rc.1'     # release tag (default: the latest release, which
#                                                 # skips pre-releases, so a pre-release needs this)
#   $env:LOCAL_SERVER_PREFIX = 'C:\Tools\local-server'   # install folder
#                                                 # (default: %LOCALAPPDATA%\Programs\local-server)
#   $env:LOCAL_SERVER_RELEASE_BASE = '<folder URL>'      # where the release files are (any URL
#                                                 # curl.exe reads); default: built from the version
#   $env:LOCAL_SERVER_OLLAMA_WAIT = '30'          # seconds to wait for Ollama after installing it
#   $env:LS_DATA_DIR                              # data folder, as for `local-server serve`
#   $env:LOCAL_SERVER_TEST_ANSWERS                # test hook of scripts/test/install.Tests.ps1: a
#                                                 # file with one answer per line, used instead of
#                                                 # the keyboard.
#
# All logic is in functions and the main function is called on the last line, so a truncated download
# cannot run half a script. Setting $env:LOCAL_SERVER_INSTALL_NO_MAIN defines the functions only (the
# Pester tests do that).
#
# Rules this script keeps (the same as install.sh):
#   - F-07: files are fetched with curl.exe only (it ships with Windows 10 and later), which sets no
#     "downloaded from the internet" mark. The binary is never changed after the download.
#   - The SHA-256 of the binary is checked against SHA256SUMS before anything is installed or changed.
#   - It asks before it changes anything outside the install folder (user PATH, Ollama, Tailscale,
#     data folder). Every question defaults to no. With no keyboard to ask, every question counts as
#     no and the manual steps are printed at the end.
#   - It does not run the first run. The first `local-server serve` shows a one-time recovery code and
#     must be run by the person, in their own window (D-101, D-149), so a fresh install does not
#     start the service: the manual steps say how.
#
# URLs this script relies on, checked with `curl -sI` on 2026-11-15 (only asked, never run):
#   https://ollama.com/download/OllamaSetup.exe   307 -> github.com/ollama/ollama/releases/latest/download/OllamaSetup.exe (302, then 200)
#   https://ollama.com/download                   200 (download page)
#   https://tailscale.com/download/windows        200 (download page)
#   winget id Tailscale.Tailscale: the manifest folder exists in microsoft/winget-pkgs (200)
# Not observed: that `OllamaSetup.exe /SILENT` installs per user without a prompt and starts Ollama
# afterwards. That is read from Ollama's ollama.iss (PrivilegesRequired=lowest, default folder
# {localappdata}\Programs\Ollama), not seen on a machine (D-49). The script therefore waits for
# Ollama to answer and shows the start step when it does not.

$script:LsReleasesUrl = 'https://github.com/inCoderKaka/local-server/releases'
$script:LsOllamaSetupUrl = 'https://ollama.com/download/OllamaSetup.exe'
$script:LsOllamaDownloadUrl = 'https://ollama.com/download'
$script:LsTailscaleDownloadUrl = 'https://tailscale.com/download/windows'
$script:LsDbFile = 'local-server.db'
$script:LsAnswerIndex = 0
$script:LsAskSkipped = $false
$script:LsSkipped = @()
$script:LsTmpDir = $null
$script:LsFailed = $false
$script:LsInstallDir = ''
$script:LsBin = ''
$script:LsCmd = 'local-server'

# --- small helpers ------------------------------------------------------------------------------

function Reset-LsState {
    $script:LsAnswerIndex = 0
    $script:LsAskSkipped = $false
    $script:LsSkipped = @()
    $script:LsFailed = $false
    $script:LsCmd = 'local-server'
}

function Stop-LsInstall {
    param([string]$Message)
    throw $Message
}

function Get-LsSkippedKeys {
    return @($script:LsSkipped)
}

function Get-LsInstallDir {
    if (-not [string]::IsNullOrEmpty($env:LOCAL_SERVER_PREFIX)) {
        return $env:LOCAL_SERVER_PREFIX.TrimEnd('\')
    }
    return (Join-Path $env:LOCALAPPDATA 'Programs\local-server')
}

# The same place `local-server serve` uses on Windows: %AppData%\local-server, or LS_DATA_DIR.
function Get-LsDataDir {
    if (-not [string]::IsNullOrEmpty($env:LS_DATA_DIR)) {
        return $env:LS_DATA_DIR
    }
    return (Join-Path $env:APPDATA 'local-server')
}

function ConvertTo-LsArch {
    param([string]$Name)
    switch ($Name) {
        'X64' { return 'amd64' }
        'Arm64' { return 'arm64' }
        default { throw "unsupported CPU type: $Name. Releases exist for amd64 and arm64. Nothing was installed." }
    }
}

# The CPU of the machine, as RuntimeInformation names it (X64, Arm64, ...). The environment
# variables are the fallback for a Windows PowerShell 5.1 on an old .NET Framework.
function Get-LsOsArchitecture {
    try {
        return [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    } catch {
        $arch = $env:PROCESSOR_ARCHITEW6432
        if ([string]::IsNullOrEmpty($arch)) { $arch = $env:PROCESSOR_ARCHITECTURE }
        switch ($arch) {
            'AMD64' { return 'X64' }
            'ARM64' { return 'Arm64' }
            default { return [string]$arch }
        }
    }
}

function Get-LsReleaseBase {
    param([string]$Version)
    if (-not [string]::IsNullOrEmpty($env:LOCAL_SERVER_RELEASE_BASE)) {
        return $env:LOCAL_SERVER_RELEASE_BASE.TrimEnd('/')
    }
    if (-not [string]::IsNullOrEmpty($Version)) {
        return ($script:LsReleasesUrl + '/download/' + $Version)
    }
    return ($script:LsReleasesUrl + '/latest/download')
}

# --- native programs ----------------------------------------------------------------------------

# Runs a program with its output on screen and returns its exit code.
function Invoke-LsProgram {
    param([string]$FilePath, [string[]]$ArgumentList = @())
    $ErrorActionPreference = 'Continue'
    try {
        & $FilePath @ArgumentList | Out-Host
    } catch {
        Write-Host $_.Exception.Message
        return 1
    }
    if ($null -eq $LASTEXITCODE) { return 0 }
    return $LASTEXITCODE
}

# Runs a program and returns its exit code and its standard output.
function Get-LsProgramOutput {
    param([string]$FilePath, [string[]]$ArgumentList = @())
    $ErrorActionPreference = 'Continue'
    $text = ''
    try {
        $text = (& $FilePath @ArgumentList 2>$null | Out-String)
    } catch {
        return [pscustomobject]@{ ExitCode = 1; Text = '' }
    }
    $code = 0
    if ($null -ne $LASTEXITCODE) { $code = $LASTEXITCODE }
    return [pscustomobject]@{ ExitCode = $code; Text = $text }
}

function Get-LsVersionText {
    param([string]$FilePath)
    $r = Get-LsProgramOutput -FilePath $FilePath -ArgumentList @('--version')
    if ($r.ExitCode -ne 0) { return '' }
    return $r.Text.Trim()
}

function Test-LsCurlPresent {
    return ($null -ne (Get-Command 'curl.exe' -ErrorAction SilentlyContinue))
}

# Downloads with curl.exe and returns its exit code. HttpsOnly turns on the https-only rule.
function Invoke-LsCurl {
    param([string]$Url, [string]$OutFile, [bool]$HttpsOnly)
    $ErrorActionPreference = 'Continue'
    $argList = @('-fsSL', '--retry', '2', '--connect-timeout', '20', '-o', $OutFile)
    if ($HttpsOnly) {
        $argList += @('--proto', '=https', '--proto-redir', '=https')
    }
    $argList += $Url
    $null = & 'curl.exe' @argList
    return $LASTEXITCODE
}

function Save-LsReleaseFile {
    param([string]$Base, [string]$Name, [string]$OutFile)
    $url = $Base + '/' + $Name
    $https = [string]::IsNullOrEmpty($env:LOCAL_SERVER_RELEASE_BASE)
    $rc = Invoke-LsCurl -Url $url -OutFile $OutFile -HttpsOnly $https
    if ($rc -ne 0) {
        Write-Host "install.ps1: could not download $url (curl exit code $rc)."
        # 22: the server answered 404 (or another error status); 37: a file:// file that is not there.
        if ($rc -eq 22 -or $rc -eq 37) {
            Write-Host 'The release or the file was not found. The "latest" release skips pre-releases, so'
            Write-Host 'when only a pre-release exists, name its tag: $env:LOCAL_SERVER_VERSION = ''TAG'' and run it again.'
            Write-Host ('Tags are listed at ' + $script:LsReleasesUrl)
        }
        Stop-LsInstall 'nothing was installed or changed.'
    }
}

# --- checksum -----------------------------------------------------------------------------------

# The hash on the SHA256SUMS line for Name, or $null. A line is "<64 hex digits>  <name>".
function Get-LsExpectedHash {
    param([string]$SumsText, [string]$Name)
    foreach ($line in ($SumsText -split "`r?`n")) {
        if ($line -match '^\s*([0-9A-Fa-f]{64})\s+\*?(\S+)\s*$') {
            if ($Matches[2] -eq $Name) { return $Matches[1] }
        }
    }
    return $null
}

function Test-LsHashEqual {
    param([string]$Expected, [string]$Actual)
    return ($Expected -ieq $Actual)
}

# Stops unless the file's SHA-256 equals the SHA256SUMS line for Name.
function Assert-LsChecksum {
    param([string]$FilePath, [string]$Name, [string]$SumsPath)
    $text = [System.IO.File]::ReadAllText($SumsPath)
    $expected = Get-LsExpectedHash -SumsText $text -Name $Name
    if ([string]::IsNullOrEmpty($expected)) {
        Stop-LsInstall "SHA256SUMS has no line for $Name, so the download cannot be verified. Nothing was installed or changed."
    }
    $actual = (Get-FileHash -LiteralPath $FilePath -Algorithm SHA256).Hash
    if (-not (Test-LsHashEqual -Expected $expected -Actual $actual)) {
        Stop-LsInstall "the SHA-256 of $Name does not match SHA256SUMS (expected $expected, got $actual). The file may be damaged or changed on the way. Nothing was installed or changed."
    }
    Write-Host "Checksum verified (SHA-256 $actual)."
}

# --- questions ----------------------------------------------------------------------------------

function Test-LsInteractive {
    if (-not [Environment]::UserInteractive) { return $false }
    try {
        if ([Console]::IsInputRedirected) { return $false }
    } catch {
        return $false
    }
    return $true
}

function Read-LsHostLine {
    param([string]$Prompt)
    return (Read-Host -Prompt $Prompt)
}

function Test-LsYes {
    param([string]$Answer)
    return ($Answer -match '^\s*(y|yes)\s*$')
}

# The next line of the test hook file, or '' when it has run out.
function Get-LsHookAnswer {
    $lines = @()
    if (Test-Path -LiteralPath $env:LOCAL_SERVER_TEST_ANSWERS) {
        $lines = @(Get-Content -LiteralPath $env:LOCAL_SERVER_TEST_ANSWERS)
    }
    $script:LsAnswerIndex = $script:LsAnswerIndex + 1
    if ($script:LsAnswerIndex -le $lines.Count) {
        return [string]$lines[$script:LsAnswerIndex - 1]
    }
    return ''
}

# True for yes. Every question defaults to no. The answer comes from the test hook file, else from
# the keyboard. With neither it is no, and LsAskSkipped says so (see Resolve-LsDecline).
function Request-LsConsent {
    param([string]$Prompt)
    $script:LsAskSkipped = $false
    $answer = ''
    if (-not [string]::IsNullOrEmpty($env:LOCAL_SERVER_TEST_ANSWERS)) {
        Write-Host "$Prompt [y/N] " -NoNewline
        $answer = Get-LsHookAnswer
        Write-Host $answer
    } elseif (Test-LsInteractive) {
        try {
            $answer = Read-LsHostLine -Prompt "$Prompt [y/N]"
        } catch {
            $answer = ''
        }
    } else {
        $script:LsAskSkipped = $true
        return $false
    }
    return (Test-LsYes -Answer $answer)
}

# The answer was no. With a keyboard, show the manual steps now. Without one, remember the key and
# show them at the end of the run, together.
function Resolve-LsDecline {
    param([string]$Key)
    if ($script:LsAskSkipped) {
        $script:LsSkipped = @($script:LsSkipped) + $Key
    } else {
        Show-LsManualSteps -Key $Key
    }
}

function Show-LsSkippedSteps {
    $keys = @(Get-LsSkippedKeys)
    if ($keys.Count -eq 0) { return }
    Write-Host ''
    Write-Host 'There was no keyboard to ask on, so these questions counted as no. Do these steps by hand:'
    foreach ($k in $keys) {
        Show-LsManualSteps -Key $k
    }
}

function Show-LsManualSteps {
    param([string]$Key)
    switch ($Key) {
        'path' {
            Write-Host ''
            Write-Host ("PATH: " + $script:LsInstallDir + " is not on your PATH. To add it, run:")
            Write-Host ("  [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';" + $script:LsInstallDir + "', 'User')")
            Write-Host '  and open a new window.'
        }
        'firstrun' {
            Write-Host ''
            Write-Host 'First run (do this once, in this window; the recovery code is only shown on a terminal):'
            Write-Host ('  1. ' + $script:LsCmd + ' serve')
            Write-Host '     It creates your data folder and shows a one-time recovery code.'
            Write-Host '  2. Write the recovery code down and keep it safe, then press Ctrl+C to stop it.'
            Write-Host ('  3. ' + $script:LsCmd + ' service start')
            Write-Host '     It starts the background service.'
        }
        'ollama' {
            Write-Host ''
            Write-Host 'Ollama: local-server shares your Ollama, so set it up first:'
            Write-Host ('  1. Install it from ' + $script:LsOllamaDownloadUrl)
            Write-Host '  2. Start it (open Ollama from the Start menu, or run: ollama serve)'
            Write-Host '  3. Pull a model to share: ollama pull <model>'
        }
        'tailscale' {
            Write-Host ''
            Write-Host 'Tailscale: it carries the encrypted connection between you and the people you invite.'
            Write-Host ('  1. Install it from ' + $script:LsTailscaleDownloadUrl + ' (or: winget install --id Tailscale.Tailscale)')
            Write-Host '  2. Open Tailscale and sign in. This script never signs in for you.'
        }
        'datadir' {
            Write-Host ''
            Write-Host ('Data folder kept: ' + (Get-LsDataDir))
            Write-Host ('To delete it (database and keys, this cannot be undone) run: Remove-Item -Recurse -Force "' + (Get-LsDataDir) + '"')
        }
    }
}

# --- user PATH ----------------------------------------------------------------------------------
# The string functions are pure. The registry is read and written only by Get-LsUserPath and
# Set-LsUserPath, which the tests replace.

function Get-LsUserPath {
    return [Environment]::GetEnvironmentVariable('Path', 'User')
}

function Set-LsUserPath {
    param([string]$Value)
    [Environment]::SetEnvironmentVariable('Path', $Value, 'User')
}

function Format-LsPathEntry {
    param([string]$Entry)
    return $Entry.Trim().TrimEnd('\')
}

function Test-LsPathHasEntry {
    param([string]$PathValue, [string]$Dir)
    $want = Format-LsPathEntry -Entry $Dir
    foreach ($e in ($PathValue -split ';')) {
        if ($e.Trim() -eq '') { continue }
        if ((Format-LsPathEntry -Entry $e) -ieq $want) { return $true }
    }
    return $false
}

function Add-LsPathEntry {
    param([string]$PathValue, [string]$Dir)
    if (Test-LsPathHasEntry -PathValue $PathValue -Dir $Dir) { return $PathValue }
    if ([string]::IsNullOrWhiteSpace($PathValue)) { return $Dir }
    return ($PathValue.TrimEnd(';') + ';' + $Dir)
}

function Remove-LsPathEntry {
    param([string]$PathValue, [string]$Dir)
    $want = Format-LsPathEntry -Entry $Dir
    $kept = @()
    foreach ($e in ($PathValue -split ';')) {
        if ($e.Trim() -eq '') { continue }
        if ((Format-LsPathEntry -Entry $e) -ieq $want) { continue }
        $kept += $e
    }
    return ($kept -join ';')
}

function Invoke-LsPathStep {
    $dir = $script:LsInstallDir
    $onUser = Test-LsPathHasEntry -PathValue (Get-LsUserPath) -Dir $dir
    $onSession = Test-LsPathHasEntry -PathValue $env:Path -Dir $dir
    if ($onUser -or $onSession) {
        $script:LsCmd = 'local-server'
        return
    }
    $script:LsCmd = ("& '" + $script:LsBin + "'")
    if (Request-LsConsent -Prompt "$dir is not on your PATH. Add it to your user PATH?") {
        Set-LsUserPath -Value (Add-LsPathEntry -PathValue (Get-LsUserPath) -Dir $dir)
        # This window too, so that `local-server` works right away.
        $env:Path = (Add-LsPathEntry -PathValue $env:Path -Dir $dir)
        $script:LsCmd = 'local-server'
        Write-Host "Added $dir to your user PATH."
    } else {
        Resolve-LsDecline -Key 'path'
    }
}

# --- binary and service -------------------------------------------------------------------------

function New-LsTempDir {
    $p = Join-Path ([System.IO.Path]::GetTempPath()) ('local-server-install-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $p | Out-Null
    $script:LsTmpDir = $p
    return $p
}

# Puts Source at Dest: a copy beside it first, then a rename over it. A program that was just
# stopped may need a moment to let go of its file, so the rename is tried a few times.
function Copy-LsBinary {
    param([string]$Source, [string]$Dest)
    $dir = Split-Path -Parent $Dest
    if (-not (Test-Path -LiteralPath $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
    $staged = Join-Path $dir ('.local-server.new.' + $PID + '.exe')
    Copy-Item -LiteralPath $Source -Destination $staged -Force
    $done = $false
    for ($i = 1; $i -le 5 -and -not $done; $i++) {
        try {
            Move-Item -LiteralPath $staged -Destination $Dest -Force
            $done = $true
        } catch {
            if ($i -eq 5) {
                Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
                Stop-LsInstall "cannot replace $Dest ($($_.Exception.Message)). The old program, if any, is still there."
            }
            Start-Sleep -Seconds 1
        }
    }
}

# What `local-server service status` said: which modes are installed, and is the user one running.
function ConvertFrom-LsServiceStatus {
    param([string]$Text)
    return [pscustomobject]@{
        UserInstalled   = ($Text -match 'user mode: installed')
        UserRunning     = ($Text -match 'user mode: installed, running')
        SystemInstalled = ($Text -match 'system mode: installed')
    }
}

function Get-LsServiceState {
    $r = Get-LsProgramOutput -FilePath $script:LsBin -ArgumentList @('service', 'status')
    return (ConvertFrom-LsServiceStatus -Text $r.Text)
}

function Invoke-LsServiceInstall {
    if (-not [string]::IsNullOrEmpty($env:LS_DATA_DIR)) {
        return (Invoke-LsProgram -FilePath $script:LsBin -ArgumentList @('service', 'install', '--data-dir', $env:LS_DATA_DIR))
    }
    return (Invoke-LsProgram -FilePath $script:LsBin -ArgumentList @('service', 'install'))
}

function Invoke-LsServiceStart {
    $rc = Invoke-LsProgram -FilePath $script:LsBin -ArgumentList @('service', 'start')
    if ($rc -ne 0) {
        Write-Host ('The background service did not start. Try: ' + $script:LsCmd + ' service start')
        $script:LsFailed = $true
    }
}

# True when `local-server doctor` reports the host rows in the database (D-101, D-102). serve
# creates the database file before it checks for a terminal, so the file alone proves nothing.
function Test-LsHostSetUp {
    param([string]$DataDir)
    $r = Get-LsProgramOutput -FilePath $script:LsBin -ArgumentList @('doctor', '--data-dir', $DataDir)
    return ([string]$r.Text -match '(?m)^\[ok  \] host: ')
}

# --- Ollama and Tailscale -----------------------------------------------------------------------

function Test-LsOllamaInstalled {
    if ($null -ne (Get-Command 'ollama.exe' -ErrorAction SilentlyContinue)) { return $true }
    return (Test-Path -LiteralPath (Join-Path $env:LOCALAPPDATA 'Programs\Ollama\ollama.exe'))
}

# The address Ollama answers on: OLLAMA_HOST, with http:// added when it has no scheme.
function Get-LsOllamaUrl {
    $h = $env:OLLAMA_HOST
    if ([string]::IsNullOrEmpty($h)) { $h = 'http://127.0.0.1:11434' }
    if ($h -notmatch '^https?://') { $h = 'http://' + $h }
    # 0.0.0.0 is where Ollama listens, not where to call it.
    $h = $h -replace '^(https?://)0\.0\.0\.0', '${1}127.0.0.1'
    return $h.TrimEnd('/')
}

function Test-LsOllamaAnswers {
    param([string]$Url)
    $ErrorActionPreference = 'Continue'
    $null = & 'curl.exe' -fsS --max-time 3 ($Url + '/api/version') 2>$null
    return ($LASTEXITCODE -eq 0)
}

function Wait-LsOllama {
    param([string]$Url, [int]$Seconds)
    $i = 0
    while ($true) {
        if (Test-LsOllamaAnswers -Url $Url) { return $true }
        if ($i -ge $Seconds) { return $false }
        Start-Sleep -Seconds 1
        $i++
    }
}

# D-49: OllamaSetup.exe /SILENT installs per user, with no administrator rights (read from ollama.iss).
function Install-LsOllama {
    $setup = Join-Path $script:LsTmpDir 'OllamaSetup.exe'
    Write-Host 'Downloading the Ollama installer...'
    $rc = Invoke-LsCurl -Url $script:LsOllamaSetupUrl -OutFile $setup -HttpsOnly $true
    if ($rc -ne 0) {
        Write-Host "The Ollama installer could not be downloaded (curl exit code $rc)."
        Show-LsManualSteps -Key 'ollama'
        return
    }
    Write-Host 'Running OllamaSetup.exe /SILENT (it installs for your user and needs no administrator rights)...'
    try {
        $p = Start-Process -FilePath $setup -ArgumentList '/SILENT' -Wait -PassThru
        if ($p.ExitCode -ne 0) {
            Write-Host ('The Ollama installer ended with exit code ' + $p.ExitCode + '.')
            Show-LsManualSteps -Key 'ollama'
        } else {
            Write-Host "The Ollama installer finished."
        }
    } catch {
        Write-Host ('The Ollama installer could not run: ' + $_.Exception.Message)
        Show-LsManualSteps -Key 'ollama'
    }
}

function Invoke-LsOllamaStep {
    $wait = 0
    Write-Host ''
    if (Test-LsOllamaInstalled) {
        Write-Host 'Ollama: found.'
    } else {
        Write-Host 'Ollama: not found. local-server shares the models of your Ollama.'
        if (Request-LsConsent -Prompt 'Install Ollama now?') {
            Install-LsOllama
            $wait = 30
            $parsed = 0
            if ([int]::TryParse([string]$env:LOCAL_SERVER_OLLAMA_WAIT, [ref]$parsed)) { $wait = $parsed }
        } else {
            Resolve-LsDecline -Key 'ollama'
        }
    }
    if (Test-LsOllamaInstalled) {
        $url = Get-LsOllamaUrl
        if (Wait-LsOllama -Url $url -Seconds $wait) {
            Write-Host "Ollama answers at $url."
        } else {
            Write-Host "Ollama is installed but does not answer at $url. Start it, then carry on:"
            Write-Host '  open Ollama from the Start menu (or run: ollama serve)'
        }
    }
}

function Test-LsTailscaleInstalled {
    if ($null -ne (Get-Command 'tailscale.exe' -ErrorAction SilentlyContinue)) { return $true }
    return (Test-Path -LiteralPath (Join-Path $env:ProgramFiles 'Tailscale\tailscale.exe'))
}

function Invoke-LsTailscaleStep {
    Write-Host ''
    if (Test-LsTailscaleInstalled) {
        Write-Host 'Tailscale: found.'
        return
    }
    Write-Host 'Tailscale: not found.'
    if (Request-LsConsent -Prompt 'Install Tailscale now?') {
        if ($null -ne (Get-Command 'winget' -ErrorAction SilentlyContinue)) {
            Write-Host 'Running: winget install --id Tailscale.Tailscale (Windows may ask for administrator approval)'
            $rc = Invoke-LsProgram -FilePath 'winget' -ArgumentList @('install', '--id', 'Tailscale.Tailscale')
            if ($rc -eq 0) {
                Write-Host 'Tailscale is installed. Signing in is your step: open Tailscale and sign in (this script never does).'
            } else {
                Write-Host "winget did not finish (exit code $rc)."
                Show-LsManualSteps -Key 'tailscale'
            }
        } else {
            Write-Host 'winget was not found, so Tailscale cannot be installed from here.'
            Show-LsManualSteps -Key 'tailscale'
        }
    } else {
        Resolve-LsDecline -Key 'tailscale'
    }
}

# --- actions ------------------------------------------------------------------------------------

function Invoke-LsFreshInstall {
    param([string]$NewBin, [string]$DataDir)
    Copy-LsBinary -Source $NewBin -Dest $script:LsBin
    Write-Host ('Installed ' + $script:LsBin + ' (' + (Get-LsVersionText -FilePath $script:LsBin) + ').')
    Invoke-LsPathStep
    Write-Host ''
    Write-Host 'Registering the background service (for your user, starts at logon)...'
    $rc = Invoke-LsServiceInstall
    $serviceReady = ($rc -eq 0)
    if (-not $serviceReady) {
        Write-Host ('The background service could not be registered. ' + $script:LsBin + ' is installed; fix the problem above, then run: ' + $script:LsCmd + ' service install')
        $script:LsFailed = $true
    }
    Invoke-LsOllamaStep
    Invoke-LsTailscaleStep
    Write-Host ''
    if ((Test-Path -LiteralPath (Join-Path $DataDir $script:LsDbFile)) -and (Test-LsHostSetUp -DataDir $DataDir)) {
        Write-Host "Found your set-up host in $DataDir; the first run is not needed."
        if ($serviceReady) {
            Invoke-LsServiceStart
        }
    } else {
        # No first run from here (D-101, D-149): the recovery code is for a person at a window.
        Show-LsManualSteps -Key 'firstrun'
    }
    Show-LsSkippedSteps
    Write-Host ''
    if ($script:LsFailed) {
        Write-Host 'local-server is installed, but a step above failed.'
        return 1
    }
    Write-Host 'Done. To remove it later: $env:LOCAL_SERVER_ACTION = ''uninstall'' and run the installer again.'
    return 0
}

function Invoke-LsUpgrade {
    param([string]$NewBin, [string]$DataDir)
    $newVer = Get-LsVersionText -FilePath $NewBin
    $oldVer = Get-LsVersionText -FilePath $script:LsBin
    if ((-not [string]::IsNullOrEmpty($newVer)) -and ($newVer -eq $oldVer)) {
        Write-Host "Already up to date ($oldVer)."
        return 0
    }
    if ([string]::IsNullOrEmpty($oldVer)) { $oldVer = 'installed version unknown' }
    if ([string]::IsNullOrEmpty($newVer)) { $newVer = 'new version unknown' }
    Write-Host "Upgrading: $oldVer -> $newVer"
    $state = Get-LsServiceState
    if ($state.UserRunning) {
        Write-Host 'Stopping the background service...'
        $rc = Invoke-LsProgram -FilePath $script:LsBin -ArgumentList @('service', 'stop')
        if ($rc -ne 0) { Write-Host 'The background service did not stop; the replace below may need a retry.' }
    }
    Copy-LsBinary -Source $NewBin -Dest $script:LsBin
    Write-Host ('Installed ' + $script:LsBin + ' (' + (Get-LsVersionText -FilePath $script:LsBin) + ').')
    if ($state.UserRunning) {
        if (Test-Path -LiteralPath (Join-Path $DataDir $script:LsDbFile)) {
            Write-Host 'Starting the background service...'
            Invoke-LsServiceStart
        } else {
            Write-Host ("The data folder was not found at $DataDir, so the service was not started. Do the first run: " + $script:LsCmd + ' serve')
        }
    } elseif ($state.UserInstalled) {
        Write-Host ('The background service is stopped; start it with: ' + $script:LsBin + ' service start')
    } elseif (-not $state.SystemInstalled) {
        Write-Host ('No background service is registered. To add it: ' + $script:LsBin + ' service install')
    }
    if ($state.SystemInstalled) {
        Write-Host 'A system mode service is installed. To run the new program, restart it from an elevated window:'
        Write-Host ('  & ''' + $script:LsBin + ''' service stop --mode system; & ''' + $script:LsBin + ''' service start --mode system')
    }
    if ($script:LsFailed) { return 1 }
    Write-Host 'Upgrade done. Your data was not touched.'
    return 0
}

function Invoke-LsInstallOrUpgrade {
    param([string]$Action, [string]$Version)
    if (-not (Test-LsCurlPresent)) {
        Stop-LsInstall 'curl.exe is needed and was not found (it ships with Windows 10 and later).'
    }
    $arch = ConvertTo-LsArch -Name (Get-LsOsArchitecture)
    $script:LsInstallDir = Get-LsInstallDir
    $script:LsBin = Join-Path $script:LsInstallDir 'local-server.exe'
    $dataDir = Get-LsDataDir
    $base = Get-LsReleaseBase -Version $Version
    $existing = Test-Path -LiteralPath $script:LsBin
    if ($Action -eq 'upgrade' -and -not $existing) {
        Stop-LsInstall ('local-server is not installed in ' + $script:LsInstallDir + ', so there is nothing to upgrade. Run the installer without upgrade to install.')
    }
    if ($Action -eq 'install' -and $existing) {
        Write-Host ('local-server is already installed at ' + $script:LsBin + ', so this is an upgrade.')
        $Action = 'upgrade'
    }
    $tmp = New-LsTempDir
    $asset = "local-server_windows_$arch.exe"
    $sums = Join-Path $tmp 'SHA256SUMS'
    $download = Join-Path $tmp $asset
    Write-Host "Downloading $asset from $base ..."
    Save-LsReleaseFile -Base $base -Name $asset -OutFile $download
    Save-LsReleaseFile -Base $base -Name 'SHA256SUMS' -OutFile $sums
    Assert-LsChecksum -FilePath $download -Name $asset -SumsPath $sums
    if ($Action -eq 'upgrade') {
        return (Invoke-LsUpgrade -NewBin $download -DataDir $dataDir)
    }
    return (Invoke-LsFreshInstall -NewBin $download -DataDir $dataDir)
}

function Invoke-LsUninstall {
    $script:LsInstallDir = Get-LsInstallDir
    $script:LsBin = Join-Path $script:LsInstallDir 'local-server.exe'
    $dataDir = Get-LsDataDir
    if (Test-Path -LiteralPath $script:LsBin) {
        $r = Get-LsProgramOutput -FilePath $script:LsBin -ArgumentList @('service', 'status')
        $state = ConvertFrom-LsServiceStatus -Text $r.Text
        if ($r.ExitCode -ne 0 -or $state.UserInstalled) {
            Write-Host 'Stopping and removing the background service...'
            $rc = Invoke-LsProgram -FilePath $script:LsBin -ArgumentList @('service', 'stop')
            if ($rc -ne 0) { Write-Host 'Could not stop the background service.' }
            $rc = Invoke-LsProgram -FilePath $script:LsBin -ArgumentList @('service', 'uninstall')
            if ($rc -ne 0) { Write-Host ('Could not remove the background service; try: & ''' + $script:LsBin + ''' service uninstall') }
        } else {
            Write-Host 'No user mode background service is registered.'
        }
        if ($state.SystemInstalled) {
            Write-Host 'A system mode service is installed. Removing it needs an elevated window, so run there:'
            Write-Host ('  & ''' + $script:LsBin + ''' service stop --mode system')
            Write-Host ('  & ''' + $script:LsBin + ''' service uninstall --mode system')
        }
        Remove-Item -LiteralPath $script:LsBin -Force
        Write-Host ('Removed ' + $script:LsBin + '.')
    } else {
        Write-Host ($script:LsBin + ' was not there, so the background service cannot be asked. If one is left, it is registered under the name local-server.')
    }
    # The default folder is ours: remove it when it is empty. A folder the user chose stays.
    if ([string]::IsNullOrEmpty($env:LOCAL_SERVER_PREFIX) -and (Test-Path -LiteralPath $script:LsInstallDir)) {
        if (@(Get-ChildItem -LiteralPath $script:LsInstallDir -Force).Count -eq 0) {
            Remove-Item -LiteralPath $script:LsInstallDir -Force
        }
    }
    $current = Get-LsUserPath
    if (Test-LsPathHasEntry -PathValue $current -Dir $script:LsInstallDir) {
        Set-LsUserPath -Value (Remove-LsPathEntry -PathValue $current -Dir $script:LsInstallDir)
        Write-Host 'Removed the install folder from your user PATH.'
    }
    Write-Host ''
    if (Test-Path -LiteralPath $dataDir) {
        Write-Host "Data folder: $dataDir (database and keys)"
        if (Request-LsConsent -Prompt 'Also delete the data folder (database, keys)? This cannot be undone.') {
            $full = [System.IO.Path]::GetFullPath($dataDir).TrimEnd('\')
            $root = [System.IO.Path]::GetPathRoot($full).TrimEnd('\')
            $profileDir = ([string]$env:USERPROFILE).TrimEnd('\')
            if ($full -ieq $root -or $full -ieq $profileDir) {
                Stop-LsInstall "refusing to delete $dataDir"
            }
            Remove-Item -LiteralPath $dataDir -Recurse -Force
            Write-Host "Deleted $dataDir."
        } else {
            Resolve-LsDecline -Key 'datadir'
        }
    } else {
        Write-Host "No data folder at $dataDir."
    }
    Write-Host 'Ollama and Tailscale were left alone.'
    Show-LsSkippedSteps
    Write-Host 'local-server is uninstalled.'
    return 0
}

function Install-LocalServerMain {
    $ErrorActionPreference = 'Stop'
    try {
        Reset-LsState
        $action = $env:LOCAL_SERVER_ACTION
        if ([string]::IsNullOrEmpty($action)) { $action = 'install' }
        $action = $action.ToLowerInvariant()
        if (@('install', 'upgrade', 'uninstall') -notcontains $action) {
            throw "LOCAL_SERVER_ACTION must be install, upgrade or uninstall (got '$action')."
        }
        $version = [string]$env:LOCAL_SERVER_VERSION
        # The tag goes into a URL: keep it to the characters a tag has.
        if ($version -ne '' -and $version -notmatch '^[A-Za-z0-9._-]+$') {
            throw 'LOCAL_SERVER_VERSION may only contain letters, digits, dots, dashes and underscores.'
        }
        if (-not [string]::IsNullOrEmpty($env:LOCAL_SERVER_PREFIX) -and -not [System.IO.Path]::IsPathRooted($env:LOCAL_SERVER_PREFIX)) {
            throw "LOCAL_SERVER_PREFIX must be an absolute path (got $env:LOCAL_SERVER_PREFIX)."
        }
        if ($action -eq 'uninstall') {
            return (Invoke-LsUninstall)
        }
        return (Invoke-LsInstallOrUpgrade -Action $action -Version $version)
    } catch {
        Write-Host ('install.ps1: ' + $_.Exception.Message) -ForegroundColor Red
        return 1
    } finally {
        if ($script:LsTmpDir -and (Test-Path -LiteralPath $script:LsTmpDir)) {
            Remove-Item -LiteralPath $script:LsTmpDir -Recurse -Force -ErrorAction SilentlyContinue
        }
        $script:LsTmpDir = $null
    }
}

if (-not $env:LOCAL_SERVER_INSTALL_NO_MAIN) {
    # Anything a step leaks into the output comes before the exit code, so take the last value.
    $lsExit = @(Install-LocalServerMain)[-1]
    # No `exit` under `irm | iex`: it would close the window. Run as a file, the exit code is real.
    if ($PSCommandPath -and $lsExit -ne 0) { exit $lsExit }
}
