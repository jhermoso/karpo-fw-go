# Starts the database servers and runs the integration suite against all of them.
# Usage: ./run.ps1            (keeps the containers running)
#        ./run.ps1 -Down      (removes them afterwards)
#        ./run.ps1 -Slot 1    (its own Compose project and host ports +10 per slot, so that
#                              several checkouts or sessions can run at the same time)
#        ./run.ps1 -Run TestProductsAndInventoryContexts   (only the tests matching the pattern)
param([switch]$Down, [int]$Slot = 0, [string]$Run = '')

$ErrorActionPreference = 'Stop'
$code = 1
$pg = 55433 + 10 * $Slot; $mssql = 51433 + 10 * $Slot; $oracle = 51521 + 10 * $Slot; $mysql = 53306 + 10 * $Slot
$project = if ($Slot -eq 0) { 'integration' } else { "integration-s$Slot" }
Push-Location $PSScriptRoot
try {
    $env:KARPO_PG_PORT = $pg; $env:KARPO_MSSQL_PORT = $mssql; $env:KARPO_ORACLE_PORT = $oracle; $env:KARPO_MYSQL_PORT = $mysql
    docker compose -p $project up -d --wait
    $env:KARPO_PG_DSN = "postgres://karpo:karpo@localhost:$pg/karpo?sslmode=disable"
    $env:KARPO_MSSQL_DSN = "sqlserver://sa:Karpo_2026!@localhost:$($mssql)?database=master"
    $env:KARPO_ORACLE_DSN = "oracle://karpo:karpo@localhost:$oracle/FREEPDB1"
    $env:KARPO_MYSQL_DSN = "karpo:karpo@tcp(localhost:$mysql)/karpo?parseTime=true&loc=UTC"
    # The whole suite takes about half an hour, far longer than the 10 minutes go test allows by default.
    if ($Run) { go test -count=1 -timeout 60m -v -run $Run ./... } else { go test -count=1 -timeout 60m -v ./... }
    $code = $LASTEXITCODE
}
finally {
    if ($Down) { docker compose -p $project down -v }
    Pop-Location
}
# The exit code is the tests' one, not that of the last docker command.
exit $code
