<#
.SYNOPSIS
  Install, update or remove the AgentNexus host daemon (nexusd) and terminal
  client (nexus) on Windows without a Go toolchain.

.DESCRIPTION
  Official GitHub releases are the default source. The script downloads the
  release archive, verifies its SHA-256, optionally verifies the cosign bundle
  and installs both binaries into the per-user directory. Existing host
  identity, config and native agent stores are never touched.

  Development workflow artifacts remain available with -Development; they
  require an authenticated GitHub CLI because they are not releases.

.PARAMETER Hub
  Public hub URL, for example https://nexus.example.com. Required with -Code.

.PARAMETER Code
  One-time pairing code from Hosts -> Pair a host. An existing pairing is kept
  and no new code is needed for an upgrade.

.PARAMETER Name
  Host name shown in the hub. Defaults to the computer name.

.PARAMETER Channel
  stable (default), prerelease or development.

.PARAMETER Version
  Install a specific release tag, for example v0.1.0.

.PARAMETER Development
  Use the newest successful GitHub Actions development build instead of a
  release. Requires gh auth login.

.PARAMETER NoService
  Only install the binaries; do not install the per-user scheduled task.

.PARAMETER RequireSignature
  Refuse to install unless a cosign bundle verifies successfully.

.PARAMETER AllowChecksumOnly
  Configure host auto-updates to accept checksum-only verification. This is an
  explicit weaker trust mode.

.PARAMETER Force
  Allow replacing the daemon while coding-agent runs are active. The script
  still asks for an explicit FORCE confirmation.

.PARAMETER Uninstall
  Stop the service and remove nexusd and nexus.

.PARAMETER PurgeConfig
  With -Uninstall, also remove the AgentNexus host config and pairing.
#>
[CmdletBinding()]
param(
  [string]$Hub = "",
  [string]$Code = "",
  [string]$Name = $env:COMPUTERNAME,
  [string]$Repo = "AlexanderWagnerDev/agent-nexus",
  [ValidateSet("stable", "prerelease", "development")]
  [string]$Channel = "stable",
  [string]$Version = "",
  [switch]$Development,
  [switch]$NoService,
  [switch]$RequireSignature,
  [switch]$AllowChecksumOnly,
  [switch]$Force,
  [switch]$Uninstall,
  [switch]$PurgeConfig
)

$ErrorActionPreference = "Stop"
if ($Channel -eq "development") { $Development = $true }
$Api = if ($env:NEXUS_UPDATE_API) { $env:NEXUS_UPDATE_API.TrimEnd("/") } else { "https://api.github.com" }

function Fail([string]$Message) {
  Write-Error $Message
  exit 1
}

if ($Development -and $Version) {
  Fail "-Version cannot be combined with -Development or -Channel development."
}

function Get-Headers {
  $headers = @{ "Accept" = "application/vnd.github+json"; "User-Agent" = "AgentNexus-Installer" }
  if ($env:NEXUS_GITHUB_TOKEN) { $headers["Authorization"] = "Bearer $env:NEXUS_GITHUB_TOKEN" }
  return $headers
}

function Download-File([string]$Uri, [string]$Path) {
  $headers = @{}
  if ($env:NEXUS_GITHUB_TOKEN -and ($Uri -match '^https://api\.github\.com/' -or $Uri -match '^https://github\.com/')) {
    $headers["Authorization"] = "Bearer $env:NEXUS_GITHUB_TOKEN"
  }
  Invoke-WebRequest -Uri $Uri -OutFile $Path -Headers $headers -UseBasicParsing -TimeoutSec 300
}

# Returns $true when the asset exists, $false only for a definitive HTTP 404,
# and aborts on any other failure. A transport error must never silently fall
# back to checksum-only verification.
function Download-FileIfPresent([string]$Uri, [string]$Path) {
  try {
    Download-File $Uri $Path
    return $true
  } catch {
    $status = -1
    try {
      if ($_.Exception.Response -and $_.Exception.Response.StatusCode) {
        $status = [int]$_.Exception.Response.StatusCode
      }
    } catch { }
    if ($status -eq 404) { return $false }
    Fail "Cannot download $Uri (HTTP $status): $($_.Exception.Message)"
  }
}

function Get-ReleaseTag {
  if ($Version) {
    if ($Version -notmatch '^v') { return "v$Version" }
    return $Version
  }
  $uri = if ($Channel -eq "stable") {
    "$Api/repos/$Repo/releases/latest"
  } else {
    "$Api/repos/$Repo/releases?per_page=30"
  }
  try {
    $result = Invoke-RestMethod -Uri $uri -Headers (Get-Headers) -TimeoutSec 30
  } catch {
    Fail "No published release found for the $Channel channel. Use -Development for a workflow build, or wait for the first official release."
  }
  if ($Channel -eq "stable") { return $result.tag_name }
  $release = @($result) | Where-Object { -not $_.draft -and $_.tag_name } | Select-Object -First 1
  if (-not $release) { Fail "No published release found for the $Channel channel." }
  return $release.tag_name
}

$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
  "X64" { "amd64" }
  "Arm64" { "arm64" }
  default { Fail "Unsupported Windows architecture: $_" }
}

$dir = Join-Path $env:LOCALAPPDATA "AgentNexus"
$dest = Join-Path $dir "nexusd.exe"
$cli = Join-Path $dir "nexus.exe"
$taskName = "AgentNexus Host"

if ($Uninstall) {
  if (Test-Path $dest) { & $dest service uninstall | Out-Null }
  Remove-Item -Force -ErrorAction SilentlyContinue $dest, $cli, "$dest.previous", "$cli.previous"
  Write-Host "Removed $dest and $cli"
  if ($PurgeConfig) {
    $configDir = Join-Path $env:APPDATA "AgentNexus"
    if (Test-Path $configDir) { Remove-Item -Recurse -Force $configDir }
    Write-Host "Removed AgentNexus host config at $configDir"
  } else {
    Write-Host "The AgentNexus host config and pairing were kept. Native agent data was not touched."
  }
  exit 0
}

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("agentnexus-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $temp | Out-Null

try {
  $version = ""
  if ($Development -or $Channel -eq "development") {
    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
      Fail "GitHub CLI (gh) is required for -Development. Install it from https://cli.github.com and run: gh auth login"
    }
    Write-Host "Looking up the newest successful development build on $Repo..."
    $runJson = gh run list --repo $Repo --workflow dev-build.yml --branch main --status success --limit 1 --json databaseId,headSha
    if (-not $runJson -or $runJson.Trim() -eq "[]") { Fail "No successful dev-build run found." }
    $run = ($runJson | ConvertFrom-Json)[0]
    $artifact = "nexusd-windows-$arch"
    gh run download $run.databaseId --repo $Repo --name $artifact --dir $temp
    $exe = Get-ChildItem -Path $temp -Recurse -Filter "nexusd*.exe" | Select-Object -First 1
    if (-not $exe) { Fail "The artifact did not contain nexusd.exe." }
    $checksum = Get-ChildItem -Path $temp -Recurse -Filter "*.sha256" | Select-Object -First 1
    if (-not $checksum) { Fail "The artifact did not contain a SHA-256 checksum." }
    $expected = (Get-Content $checksum.FullName -Raw).Split(" ")[0].Trim().ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 -Path $exe.FullName).Hash.ToLowerInvariant()
    if ($expected -ne $actual) { Fail "SHA-256 mismatch for the downloaded daemon." }
    Copy-Item $exe.FullName (Join-Path $temp "nexusd.exe") -Force
    $version = "development"
  } else {
    $tag = Get-ReleaseTag
    if (-not $tag) { Fail "Could not determine a release tag." }
    $version = $tag -replace '^v', ''
    $archiveName = "agent-nexus_${version}_windows_${arch}.zip"
    $base = "https://github.com/$Repo/releases/download/$tag"
    Write-Host "Downloading $archiveName ($Channel)..."
    Download-File "$base/$archiveName" (Join-Path $temp $archiveName)
    Download-File "$base/checksums.txt" (Join-Path $temp "checksums.txt")

    $expected = $null
    foreach ($line in Get-Content (Join-Path $temp "checksums.txt")) {
      $fields = $line -split '\s+'
      if ($fields.Count -ge 2 -and $fields[1] -eq $archiveName) { $expected = $fields[0].ToLowerInvariant(); break }
    }
    if (-not $expected) { Fail "checksums.txt does not list $archiveName" }
    $actual = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $temp $archiveName)).Hash.ToLowerInvariant()
    if ($expected -ne $actual) { Fail "SHA-256 mismatch for $archiveName" }

    if (Get-Command cosign -ErrorAction SilentlyContinue) {
      $bundle = Join-Path $temp "checksums.txt.sigstore.json"
      $hasBundle = Download-FileIfPresent "$base/checksums.txt.sigstore.json" $bundle
      if (-not $hasBundle) {
        if ($RequireSignature) { Fail "The release has no cosign bundle and -RequireSignature was set." }
        Write-Warning "The release has no cosign bundle; SHA-256 only."
      } else {
        & cosign verify-blob --bundle $bundle `
          --certificate-identity-regexp "^https://github.com/$Repo/.github/workflows/release.yml@refs/tags/" `
          --certificate-oidc-issuer "https://token.actions.githubusercontent.com" `
          (Join-Path $temp "checksums.txt") | Out-Null
        if ($LASTEXITCODE -ne 0) { Fail "cosign verification failed." }
        Write-Host "Checksum and cosign signature verified."
      }
    } elseif ($RequireSignature) {
      Fail "cosign is required by -RequireSignature but was not found."
    } else {
      Write-Warning "cosign is not installed; the archive was verified by SHA-256 only."
    }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [System.IO.Compression.ZipFile]::OpenRead((Join-Path $temp $archiveName))
    try {
      foreach ($entry in $zip.Entries) {
        $name = [System.IO.Path]::GetFileName($entry.FullName)
        if ($entry.FullName.EndsWith("/") -or ($name -ne "nexusd.exe" -and $name -ne "nexus.exe")) { continue }
        if ($entry.Length -gt 128MB) { Fail "archive entry exceeds the extraction limit" }
        [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, (Join-Path $temp $name), $true)
      }
    } finally {
      $zip.Dispose()
    }
  }

  $newExe = Join-Path $temp "nexusd.exe"
  $newCli = Join-Path $temp "nexus.exe"
  if (-not (Test-Path $newExe)) { Fail "The archive did not contain nexusd.exe." }
  $versionOutput = & $newExe version 2>&1
  if ($LASTEXITCODE -ne 0 -or "$versionOutput" -notmatch "^nexusd ") {
    Fail "The downloaded daemon did not pass its version check."
  }
  if ($version -and $version -ne "development") {
    $reported = ("$versionOutput" -split '\s+')[1] -replace '^v', ''
    if ($reported -ne $version) { Fail "Downloaded daemon reports $reported, expected $version." }
  }
  Write-Host "Checksum and executable verified."

  $activeRuns = 0
  $statusJson = & $newExe status --json 2>$null
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

  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  if (Test-Path $dest) { & $dest service uninstall | Out-Null }
  Start-Sleep -Milliseconds 800
  if (Test-Path $dest) { Copy-Item $dest "$dest.previous" -Force }
  if (Test-Path $cli) { Copy-Item $cli "$cli.previous" -Force }

  try {
    Copy-Item $newExe $dest -Force
    if (Test-Path $newCli) { Copy-Item $newCli $cli -Force }
    Write-Host "Installed $dest and $cli"
  } catch {
    if (Test-Path "$dest.previous") { Copy-Item "$dest.previous" $dest -Force }
    if (Test-Path "$cli.previous") { Copy-Item "$cli.previous" $cli -Force }
    Fail "Binary replacement failed; the previous installation was restored."
  }

  $configFile = Join-Path $env:APPDATA "AgentNexus\nexusd.json"
  if ($Code) {
    if (Test-Path $configFile) {
      Write-Host "Existing host pairing kept; the supplied -Code was ignored."
    } else {
      & $dest pair --hub $Hub --code $Code --name $Name
      if ($LASTEXITCODE -ne 0) {
        if (Test-Path "$dest.previous") {
          Copy-Item "$dest.previous" $dest -Force
          if (Test-Path "$cli.previous") { Copy-Item "$cli.previous" $cli -Force }
          & $dest service install | Out-Null
        }
        Fail "Pairing failed; the previous installation was restored."
      }
      Write-Host "Compare the printed Pairing SAS with the Hosts page and confirm it there."
    }
  }

  if ($version -and $version -ne "development") {
    & $dest update --configure --channel $Channel --auto on --install-when-idle on | Out-Null
    if ($AllowChecksumOnly) { & $dest update --configure --checksum-only | Out-Null }
    elseif ($RequireSignature) { & $dest update --configure --require-signature | Out-Null }
  }

  if (-not $NoService) {
    & $dest service install
    if ($LASTEXITCODE -ne 0) {
      if (Test-Path "$dest.previous") {
        Copy-Item "$dest.previous" $dest -Force
        if (Test-Path "$cli.previous") { Copy-Item "$cli.previous" $cli -Force }
        & $dest service install | Out-Null
        Fail "Service installation failed; the previous daemon was restored."
      }
      Fail "Service installation failed and no rollback binary was available."
    }
    & $dest service status
    & $dest doctor
  }

  if (-not $RequireSignature -and -not $AllowChecksumOnly -and -not (Get-Command cosign -ErrorAction SilentlyContinue)) {
    Write-Host "Note: automatic updates require cosign by default. Install cosign, or set"
    Write-Host "      checksum-only explicitly with: nexusd update --configure --checksum-only"
  }
} finally {
  Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
}
