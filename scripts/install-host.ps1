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
#>
[CmdletBinding()]
param(
  [string]$Hub = "",
  [string]$Code = "",
  [string]$Name = $env:COMPUTERNAME,
  [string]$Repo = "AlexanderWagnerDev/agent-nexus",
  [string]$Ref = "main",
  [switch]$NoService
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
  if ($checksum) {
    $expected = (Get-Content $checksum.FullName -Raw).Split(" ")[0].Trim().ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 -Path $exe.FullName).Hash.ToLowerInvariant()
    if ($expected -ne $actual) { Fail "SHA-256 mismatch for the downloaded daemon." }
    Write-Host "Checksum verified."
  }

  $dir = Join-Path $env:LOCALAPPDATA "AgentNexus"
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $dest = Join-Path $dir "nexusd.exe"
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
    if ($LASTEXITCODE -ne 0) { Fail "Service installation failed." }
    & $dest service status
    & $dest doctor
  }
} finally {
  Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
}
