$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
go run ./cmd/vivurouter-go
