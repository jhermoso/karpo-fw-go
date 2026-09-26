# Starts the database servers and runs the integration suite against all of them.
# Usage: ./run.ps1            (keeps the containers running)
#        ./run.ps1 -Down      (removes them afterwards)
param([switch]$Down)

$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    docker compose up -d --wait
    $env:KARPO_PG_DSN = 'postgres://karpo:karpo@localhost:55433/karpo?sslmode=disable'
    $env:KARPO_MSSQL_DSN = 'sqlserver://sa:Karpo_2026!@localhost:51433?database=master'
    $env:KARPO_ORACLE_DSN = 'oracle://karpo:karpo@localhost:51521/FREEPDB1'
    $env:KARPO_MYSQL_DSN = 'karpo:karpo@tcp(localhost:53306)/karpo?parseTime=true&loc=UTC'
    go test -count=1 -v ./...
}
finally {
    if ($Down) { docker compose down -v }
    Pop-Location
}
