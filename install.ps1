$ErrorActionPreference = "Stop"

$repo = "youngwoocho02/unity-scanner"
$installDir = "$env:LOCALAPPDATA\unity-scanner"
$exe = "$installDir\unity-scanner.exe"

New-Item -ItemType Directory -Force -Path $installDir | Out-Null

$url = "https://github.com/$repo/releases/latest/download/unity-scanner-windows-amd64.exe"
Write-Host "Downloading unity-scanner for windows/amd64..."
Invoke-WebRequest -Uri $url -OutFile $exe -UseBasicParsing

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$installDir;$userPath", "User")
    $env:Path = "$installDir;$env:Path"
    Write-Host "Added $installDir to PATH (restart shell to apply)"
}

Write-Host "Installed unity-scanner to $exe"
& $exe version
