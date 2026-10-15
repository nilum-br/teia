# Package an already-built plugin for local validation. No network, signing or publishing.
[CmdletBinding()]
param([Parameter(Mandatory = $true)][string]$Version)
$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^\d+\.\d+\.\d+-candidate\.[0-9A-Za-z.-]+$') { throw 'Use a SemVer candidate version, for example 0.1.0-candidate.20261015.1' }
$repo = Split-Path -Parent $PSScriptRoot
$source = Join-Path $repo 'plugins/grafana/dist'
$out = Join-Path $repo ('dist/candidate/' + $Version)
$plugin = Join-Path $out 'nilum-teia-panel'
if (Test-Path -LiteralPath $out) { throw 'Candidate directory already exists; choose a new version' }
if (!(Test-Path -LiteralPath (Join-Path $source 'module.js'))) { throw 'Build the plugin first' }
if (Test-Path -LiteralPath (Join-Path $source 'MANIFEST.txt')) { throw 'Do not re-version a signed bundle; use the release signing workflow' }
$metadata = Get-Content -Raw -LiteralPath (Join-Path $source 'plugin.json') | ConvertFrom-Json
if ($metadata.id -ne 'nilum-teia-panel') { throw 'Unexpected plugin identity' }
New-Item -ItemType Directory -Path $out -Force | Out-Null
Copy-Item -LiteralPath $source -Destination $plugin -Recurse
$metadata.info.version = $Version
[IO.File]::WriteAllText((Join-Path $plugin 'plugin.json'), ($metadata | ConvertTo-Json -Depth 30), (New-Object Text.UTF8Encoding $false))
$zip = Join-Path $out ('nilum-teia-panel-' + $Version + '.zip')
Compress-Archive -LiteralPath $plugin -DestinationPath $zip
$hash = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
($hash + '  ' + [IO.Path]::GetFileName($zip)) | Set-Content -LiteralPath (Join-Path $out 'checksums.txt') -Encoding ASCII
Write-Output ('Unsigned candidate: ' + $zip)
Write-Output ('SHA256: ' + $hash)
