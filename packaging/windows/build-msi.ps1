# Builds the MSI with the WiX Toolset (installed as a .NET tool if missing).
#
#   ./build-msi.ps1 -Version 1.5.0 -Exe dist/ducktainer.exe -Out dist/ducktainer-1.5.0-x64.msi
param(
  [Parameter(Mandatory)][string]$Version,
  [Parameter(Mandatory)][string]$Exe,
  [Parameter(Mandatory)][string]$Out
)
$ErrorActionPreference = 'Stop'
$wixVersion = '5.0.2'
if (-not (Get-Command wix -ErrorAction SilentlyContinue)) {
  dotnet tool install --global wix --version $wixVersion
  if ($LASTEXITCODE) { throw 'could not install wix' }
  $env:PATH += ";$env:USERPROFILE\.dotnet\tools"
}
wix build "$PSScriptRoot\ducktainer.wxs" -arch x64 -d "Version=$Version" -d "Exe=$((Resolve-Path $Exe).Path)" -o $Out
if ($LASTEXITCODE) { throw "wix build: exit $LASTEXITCODE" }
Get-Item $Out | Format-Table Name, Length
