$ErrorActionPreference = "Stop"
$base = if ($env:BASE_URL) { $env:BASE_URL } else { "http://127.0.0.1:20129" }
Write-Host "Checking $base/api/health"
Invoke-RestMethod "$base/api/health" | ConvertTo-Json -Depth 5
Write-Host "Checking $base/v1/models"
Invoke-RestMethod "$base/v1/models" | ConvertTo-Json -Depth 10
Write-Host "Checking $base/api/metrics"
Invoke-RestMethod "$base/api/metrics" | ConvertTo-Json -Depth 10
Write-Host "Checking $base/api/cooldowns"
Invoke-RestMethod "$base/api/cooldowns" | ConvertTo-Json -Depth 10
