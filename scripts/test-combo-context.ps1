param(
    [string]$BaseUrl = "http://127.0.0.1:20129",
    [string]$ApiKey = "",
    [string]$ComboModel = "combo-test",
    [int]$SmallRepeat = 20,
    [int]$LargeRepeat = 50000,
    [switch]$SkipModels
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$outDir = Join-Path $root "tmp-combo-context-test"
New-Item -ItemType Directory -Force $outDir | Out-Null

$headers = @{ "Content-Type" = "application/json" }
if ($ApiKey.Trim()) { $headers["Authorization"] = "Bearer $ApiKey" }

function Write-JsonFile([string]$Name, $Value) {
    $path = Join-Path $outDir $Name
    $json = $Value | ConvertTo-Json -Depth 12
    [IO.File]::WriteAllText($path, $json, (New-Object Text.UTF8Encoding($false)))
    return $path
}

function Invoke-Gateway([string]$Name, $Payload) {
    $path = Write-JsonFile $Name $Payload
    $uri = "$BaseUrl/v1/chat/completions"
    $started = Get-Date
    try {
        $response = Invoke-WebRequest -Uri $uri -Method Post -Headers $headers -InFile $path -UseBasicParsing
        $elapsed = ((Get-Date) - $started).TotalMilliseconds
        return [pscustomobject]@{ Name = $Name; Status = [int]$response.StatusCode; ElapsedMs = [math]::Round($elapsed); Body = $response.Content; File = $path }
    } catch {
        $elapsed = ((Get-Date) - $started).TotalMilliseconds
        $status = 0
        $body = $_.Exception.Message
        if ($_.Exception.Response) {
            $status = [int]$_.Exception.Response.StatusCode
            $reader = New-Object IO.StreamReader($_.Exception.Response.GetResponseStream())
            $body = $reader.ReadToEnd()
            $reader.Dispose()
        }
        return [pscustomobject]@{ Name = $Name; Status = $status; ElapsedMs = [math]::Round($elapsed); Body = $body; File = $path }
    }
}

Write-Host "VivuRouter Combo context test" -ForegroundColor Cyan
Write-Host "BaseUrl: $BaseUrl"
Write-Host "Combo:   $ComboModel"
Write-Host "Output:  $outDir"

if (-not $SkipModels) {
    try {
        $modelHeaders = @{}
        if ($ApiKey.Trim()) { $modelHeaders["Authorization"] = "Bearer $ApiKey" }
        $models = Invoke-WebRequest -Uri "$BaseUrl/v1/models" -Headers $modelHeaders -UseBasicParsing
        $modelsPath = Join-Path $outDir "models.json"
        $models.Content | Set-Content $modelsPath -Encoding utf8
        Write-Host "`n/v1/models saved to $modelsPath" -ForegroundColor Green
        try {
            $matching = (($models.Content | ConvertFrom-Json).data | Where-Object { $_.id -like "*$ComboModel*" })
            if ($matching) { $matching | Select-Object id, owned_by, context_length, max_context_length, max_input_tokens, max_tokens | Format-List }
            else { Write-Warning "Combo was not found in /v1/models by name." }
        } catch { Write-Warning "Could not parse /v1/models JSON: $($_.Exception.Message)" }
    } catch { Write-Warning "Could not query /v1/models: $($_.Exception.Message)" }
}

$small = @{
    model = $ComboModel
    messages = @(@{ role = "user"; content = "Reply with exactly COMBO_SMALL_OK" })
    stream = $false
    max_tokens = 32
}
$largeText = ("context-test " * $LargeRepeat)
$large = @{
    model = $ComboModel
    messages = @(@{ role = "user"; content = "Context capacity test. Do not repeat the input. $largeText" })
    stream = $false
    max_tokens = 256
}

$results = @(
    (Invoke-Gateway "combo-small.json" $small),
    (Invoke-Gateway "combo-large.json" $large)
)

foreach ($result in $results) {
    Write-Host "`n[$($result.Name)] HTTP=$($result.Status) elapsed=$($result.ElapsedMs)ms" -ForegroundColor Yellow
    Write-Host ($result.Body.Substring(0, [math]::Min(1200, $result.Body.Length)))
    if ($result.Body -match "unsupported_capability|context_limit|no provider supports") {
        Write-Warning "Planner rejected this request. Check Combo context_length and candidate model metadata."
    }
}

$summary = [pscustomobject]@{
    BaseUrl = $BaseUrl
    ComboModel = $ComboModel
    SmallRepeat = $SmallRepeat
    LargeRepeat = $LargeRepeat
    ApproxLargeCharacters = $largeText.Length
    ApproxLargeTokensAt4Chars = [math]::Round($largeText.Length / 4)
    Results = $results | Select-Object Name, Status, ElapsedMs, File, Body
}
$summary | ConvertTo-Json -Depth 12 | Set-Content (Join-Path $outDir "summary.json") -Encoding utf8
Write-Host "`nSummary saved to $(Join-Path $outDir 'summary.json')" -ForegroundColor Green
