[CmdletBinding()]
param(
    [string]$EnvFile = (Join-Path $PSScriptRoot ".env"),
    [string]$Executable = (Join-Path $PSScriptRoot "vivurouter-go.exe")
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) {
    throw "Environment file not found: $EnvFile"
}

if (-not (Test-Path -LiteralPath $Executable -PathType Leaf)) {
    throw "VivuRouter executable not found: $Executable"
}

Get-Content -LiteralPath $EnvFile | ForEach-Object {
    $line = $_.Trim()
    if (-not $line -or $line.StartsWith("#")) {
        return
    }

    $parts = $line -split "=", 2
    if ($parts.Count -ne 2) {
        throw "Invalid .env line (expected NAME=VALUE): $line"
    }

    $name = $parts[0].Trim()
    $value = $parts[1].Trim()
    if (-not $name) {
        throw "Invalid .env variable name: $line"
    }

    if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
        ($value.StartsWith("'") -and $value.EndsWith("'"))) {
        $value = $value.Substring(1, $value.Length - 2)
    }

    # Existing process variables take precedence over values in .env.
    if ($null -eq [Environment]::GetEnvironmentVariable($name, "Process")) {
        [Environment]::SetEnvironmentVariable($name, $value, "Process")
    }
}

Write-Host "Starting VivuRouter with environment from $EnvFile"
& $Executable
exit $LASTEXITCODE
