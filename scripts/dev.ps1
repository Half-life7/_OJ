[CmdletBinding()]
param(
    [ValidateSet('Auto', 'WSL', 'Native')]
    [string]$SandboxMode = 'Auto',
    [string]$WslDistribution = 'Ubuntu',
    [int]$Port = 8080,
    [int]$SandboxPort = 5050
)

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo
New-Item -ItemType Directory -Force -Path '.cache\data' | Out-Null

function Require-Command([string]$Name) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Missing dependency: $Name"
    }
}

Require-Command go
if ($SandboxMode -eq 'Auto') {
    if (Get-Command wsl.exe -ErrorAction SilentlyContinue) { $SandboxMode = 'WSL' }
    elseif (Get-Command go-judge -ErrorAction SilentlyContinue) { $SandboxMode = 'Native' }
    else { throw 'Neither WSL nor go-judge was found. Install go-judge in WSL2, or add a native go-judge to PATH for smoke testing.' }
}

if ($SandboxMode -eq 'WSL') {
    Require-Command wsl.exe
    $distroArgs = @('-d', $WslDistribution, '-u', 'root', '-e', 'bash', '-lc')
    $check = "for x in g++ python3 javac java go-judge; do command -v `$x >/dev/null || { echo MISSING:`$x >&2; exit 1; }; done; test -f /sys/fs/cgroup/cgroup.controllers || { echo CGROUP_V1 >&2; exit 2; }"
    & wsl.exe @distroArgs $check
    if ($LASTEXITCODE -eq 2) { Write-Warning 'WSL is not using cgroup v2; resource limits may not work.' }
    elseif ($LASTEXITCODE -ne 0) { throw "WSL $WslDistribution is missing g++, python3, javac, java, or go-judge." }
    try {
        & wsl.exe @distroArgs 'systemctl is-system-running >/dev/null 2>&1'
        if ($LASTEXITCODE -ne 0) { Write-Warning 'WSL systemd is not running; set [boot] systemd=true in /etc/wsl.conf.' }
    } catch {
        Write-Warning 'Could not determine WSL systemd status.'
    }
} else {
    Require-Command go-judge
    $missingTools = @()
    foreach ($item in 'g++', 'python3', 'javac', 'java') {
        if (-not (Get-Command $item -ErrorAction SilentlyContinue)) {
            $missingTools += $item
        }
    }
    if ($missingTools.Count -gt 0) {
        throw "Native Windows is missing: $($missingTools -join ', '). Use WSL2 for the supported development path."
    }
    Write-Warning 'Native Windows go-judge does not represent Linux timing or isolation, and may require Windows command paths in languages.yaml.'
}

if (-not $env:SANDBOX_TOKEN) {
    $bytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    $env:SANDBOX_TOKEN = [Convert]::ToBase64String($bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_')
}
if ($env:SANDBOX_TOKEN.Length -lt 32 -or $env:SANDBOX_TOKEN.Contains("`n")) {
    throw 'SANDBOX_TOKEN must contain at least 32 characters and no newlines.'
}

if (-not $env:JUDGE_CPUSET) { Write-Warning 'JUDGE_CPUSET is empty; timing may be unstable.' }
$workers = if ($env:JUDGE_CPUSET) { ($env:JUDGE_CPUSET -split ',').Count } else { [Math]::Max(1, [int]([Environment]::ProcessorCount / 2)) }

$sandboxUrl = "http://127.0.0.1:$SandboxPort"
$goJudgeProcess = $null
$alreadyRunning = $false
try {
    $headers = @{ Authorization = "Bearer $($env:SANDBOX_TOKEN)" }
    $null = Invoke-WebRequest -UseBasicParsing -Uri "$sandboxUrl/version" -Headers $headers -TimeoutSec 2
    $alreadyRunning = $true
} catch {}

if (-not $alreadyRunning) {
    $arguments = @(
        "-http-addr=127.0.0.1:$SandboxPort",
        "-auth-token=$($env:SANDBOX_TOKEN)",
        '-no-fallback',
        "-parallelism=$workers",
        '-time-limit-checker-interval=10ms',
        '-file-timeout=30m'
    )
    if ($SandboxMode -eq 'WSL') {
        $quotedArgs = ($arguments | ForEach-Object { "'" + $_.Replace("'", "'\''") + "'" }) -join ' '
        $command = "export SANDBOX_TOKEN='$($env:SANDBOX_TOKEN)'; exec go-judge $quotedArgs"
        $goJudgeProcess = Start-Process -FilePath 'wsl.exe' `
            -ArgumentList @('-d', $WslDistribution, '-u', 'root', '-e', 'bash', '-lc', "`"$command`"") `
            -RedirectStandardOutput '.cache\go-judge.log' `
            -RedirectStandardError '.cache\go-judge.err.log' `
            -WindowStyle Hidden -PassThru
    } else {
        $binary = (Get-Command go-judge).Source
        $goJudgeProcess = Start-Process -FilePath $binary -ArgumentList $arguments `
            -RedirectStandardOutput '.cache\go-judge.log' `
            -RedirectStandardError '.cache\go-judge.err.log' `
            -WindowStyle Hidden -PassThru
    }
    $ready = $false
    for ($i = 0; $i -lt 80; $i++) {
        Start-Sleep -Milliseconds 250
        try {
            $null = Invoke-WebRequest -UseBasicParsing -Uri "$sandboxUrl/version" -Headers $headers -TimeoutSec 1
            $ready = $true
            break
        } catch {
            if ($goJudgeProcess.HasExited) { break }
        }
    }
    if (-not $ready) {
        throw "go-judge failed to start. Inspect .cache\go-judge.log and .cache\go-judge.err.log."
    }
} else {
    Write-Host "Reusing go-judge at $sandboxUrl."
}

$env:PORT = [string]$Port
$env:DB_PATH = if ($env:DB_PATH) { $env:DB_PATH } else { '.\data\oj.db' }
$env:SANDBOX = 'gojudge'
$env:SANDBOX_URL = $sandboxUrl
$env:JUDGE_WORKERS = [string]$workers
$env:RERUN_BORDERLINE = if ($env:RERUN_BORDERLINE) { $env:RERUN_BORDERLINE } else { 'true' }
$env:ALLOW_REGISTER = if ($env:ALLOW_REGISTER) { $env:ALLOW_REGISTER } else { 'true' }
$env:COOKIE_SECURE = if ($env:COOKIE_SECURE) { $env:COOKIE_SECURE } else { 'auto' }

Write-Host "Development services: sandbox=$sandboxUrl web=http://127.0.0.1:$Port"
Write-Host 'WSL2 and Windows are for functional development only. Run cmd/bench on an exclusive-core Linux host for timing conclusions.'
try {
    & go run ./cmd/oj -dev
} finally {
    if ($goJudgeProcess -and -not $goJudgeProcess.HasExited) {
        Stop-Process -Id $goJudgeProcess.Id -ErrorAction SilentlyContinue
    }
}
