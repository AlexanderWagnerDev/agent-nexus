<#
.SYNOPSIS
  Install or update the AgentNexus host daemon (nexusd) on Windows without a Go
  toolchain.

.DESCRIPTION
  Downloads the newest development build produced by the "Dev host binaries"
  workflow. Development builds are GitHub Actions artifacts, not releases, so
  GitHub CLI authentication (gh auth login) with read access to the private
  repository is required. No token is stored in this script.

.PARAMETER Hub
  Public hub URL, for example https://nexus.example.com. Required with -Code.

.PARAMETER Code
  One-time pairing code from Hosts -> Pair a host.

.PARAMETER Name
  Host name shown in the hub. Defaults to the computer name.

.PARAMETER Repo
  GitHub repository in owner/name form.

.PARAMETER Ref
  Branch whose newest successful development build is used.

.PARAMETER NoService
  Only install the binary; do not install the per-user scheduled task.

.PARAMETER Force
  Allow replacing the daemon while coding-agent runs are active. The script
  still asks for an explicit FORCE confirmation; without this switch it
  refuses to interrupt active runs.
#>
[CmdletBinding()]
param(
  [string]$Hub = "",
  [string]$Code = "",
  [string]$Name = $env:COMPUTERNAME,
  [string]$Repo = "AlexanderWagnerDev/agent-nexus",
  [string]$Ref = "main",
  [switch]$NoService,
  [switch]$Force
)

$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
  Write-Error $Message
  exit 1
}

if ($Code -and -not $Hub) { Fail "-Hub is required when -Code is used." }
if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
  Fail @"
GitHub CLI (gh) is required because the repository is private and its
development binaries are authenticated workflow artifacts. Install it from
https://cli.github.com/ and run: gh auth login
"@
}

$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
  "X64" { "amd64" }
  "Arm64" { "arm64" }
  default { Fail "Unsupported Windows architecture: $_" }
}

Write-Host "Looking up the newest successful development build on $Repo ($Ref)..."
$runJson = gh run list --repo $Repo --workflow dev-build.yml --branch $Ref --status success --limit 1 --json databaseId,headSha
if (-not $runJson -or $runJson.Trim() -eq "[]") {
  Fail "No successful dev-build run found. Push to $Ref or run the workflow manually."
}
$run = ($runJson | ConvertFrom-Json)[0]
$artifact = "nexusd-windows-$arch"
$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("agentnexus-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $temp | Out-Null

try {
  Write-Host "Downloading $artifact from run $($run.databaseId) ($($run.headSha.Substring(0, 12)))..."
  gh run download $run.databaseId --repo $Repo --name $artifact --dir $temp
  $exe = Get-ChildItem -Path $temp -Recurse -Filter "nexusd*.exe" | Select-Object -First 1
  if (-not $exe) { Fail "The artifact did not contain nexusd.exe." }

  $checksum = Get-ChildItem -Path $temp -Recurse -Filter "*.sha256" | Select-Object -First 1
  if (-not $checksum) { Fail "The artifact did not contain a SHA-256 checksum; refusing to install." }
  $expected = (Get-Content $checksum.FullName -Raw).Split(" ")[0].Trim().ToLowerInvariant()
  $actual = (Get-FileHash -Algorithm SHA256 -Path $exe.FullName).Hash.ToLowerInvariant()
  if ($expected -ne $actual) { Fail "SHA-256 mismatch for the downloaded daemon." }
  if ($exe.Length -lt 1MB) { Fail "The downloaded daemon is unexpectedly small." }
  $versionOutput = & $exe.FullName version 2>&1
  if ($LASTEXITCODE -ne 0 -or "$versionOutput" -notmatch "^nexusd ") {
    Fail "The downloaded daemon did not pass its version check."
  }
  Write-Host "Checksum and executable verified."

  $dir = Join-Path $env:LOCALAPPDATA "AgentNexus"
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $dest = Join-Path $dir "nexusd.exe"
  $activeRuns = 0
  $statusJson = & $exe.FullName status --json 2>$null
  if ($LASTEXITCODE -eq 0 -and $statusJson) {
    try { $activeRuns = [int](($statusJson | ConvertFrom-Json).active_runs) } catch { $activeRuns = 0 }
  }
  if ($activeRuns -gt 0 -and -not $Force) {
    Fail "Refusing to replace the daemon: $activeRuns active agent run(s). Wait until idle, or pass -Force after checking the runs."
  }
  if ($activeRuns -gt 0) {
    $answer = Read-Host "Type FORCE to interrupt $activeRuns active agent run(s)"
    if ($answer -cne "FORCE") { Fail "Update cancelled; no active run was interrupted." }
  }
  if (Test-Path $dest) {
    Copy-Item $dest "$dest.previous" -Force
  }
  # The per-user service is stopped before replacing the binary so Windows does
  # not keep an open image handle. The task is reinstalled afterwards.
  $taskName = "AgentNexus Host"
  $task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
  if ($task -and $task.State -eq "Running") {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 800
  }
  Copy-Item $exe.FullName $dest -Force
  Write-Host "Installed $dest"

  if ($Code) {
    & $dest pair --hub $Hub --code $Code --name $Name
    if ($LASTEXITCODE -ne 0) { Fail "Pairing failed." }
    Write-Host "Compare the printed Pairing SAS with the Hosts page and confirm it there."
  }

  if (-not $NoService) {
    & $dest service install
    if ($LASTEXITCODE -ne 0) {
      if (Test-Path "$dest.previous") {
        Copy-Item "$dest.previous" $dest -Force
        & $dest service install | Out-Null
        Fail "Service installation failed; the previous daemon was restored."
      }
      Fail "Service installation failed and no rollback binary was available."
    }
    & $dest service status
    & $dest doctor
  }
} finally {
  Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
}
