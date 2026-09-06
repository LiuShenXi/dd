$ErrorActionPreference = 'Stop'
$privateDirectory = 'C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905'
$archivePath = Join-Path $privateDirectory 'production-snapshot.dump'
$environmentPath = Join-Path $privateDirectory 'postgres.env'
$networkName = 'carpool-v13-test-net'
$containerName = 'carpool-v13-test-postgres'
$volumeName = 'carpool-v13-test-pgdata'
if (-not (Test-Path -LiteralPath $archivePath)) { throw 'Verified private archive is missing.' }
if ((& docker ps -a --format '{{.Names}}') -contains $containerName) { throw 'Test database container already exists; refusing to overwrite.' }
if ((& docker volume ls --format '{{.Name}}') -contains $volumeName) { throw 'Test database volume already exists; refusing to overwrite.' }
if (-not ((& docker network ls --format '{{.Name}}') -contains $networkName)) {
    & docker network create --internal $networkName | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Failed to create internal test network.' }
}
$networkState = (& docker network inspect $networkName | ConvertFrom-Json)[0]
if (-not $networkState.Internal) { throw 'Test network must block external routing.' }
$testPasswordBytes = [Security.Cryptography.RandomNumberGenerator]::GetBytes(32)
$testPassword = [Convert]::ToHexString($testPasswordBytes)
$environmentLines = "POSTGRES_USER=carpool_test`nPOSTGRES_DB=carpool_test`nPOSTGRES_PASSWORD=$testPassword`n"
[IO.File]::WriteAllText($environmentPath, $environmentLines, [Text.UTF8Encoding]::new($false))
$testPassword = $null
$environmentLines = $null
& docker volume create $volumeName | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to create fresh private database volume.' }
& docker run -d --name $containerName --network $networkName --network-alias carpool-db --env-file $environmentPath --mount "type=volume,source=$volumeName,target=/var/lib/postgresql" --health-cmd 'pg_isready -U carpool_test -d carpool_test' --health-interval 2s --health-timeout 3s --health-retries 20 postgres:18-alpine | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to start isolated PostgreSQL 18.' }
$ready = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    & docker exec $containerName pg_isready -U carpool_test -d carpool_test *> $null
    if ($LASTEXITCODE -eq 0) { $ready = $true; break }
    Start-Sleep -Seconds 1
}
if (-not $ready) { throw 'Isolated database did not become ready.' }
& docker cp $archivePath "${containerName}:/tmp/production-snapshot.dump"
if ($LASTEXITCODE -ne 0) { throw 'Failed to stream the archive into the isolated container.' }
& docker exec $containerName chmod 600 /tmp/production-snapshot.dump
$restoreProcess = Start-Process -FilePath (Get-Command docker.exe).Source -ArgumentList @('exec', $containerName, 'pg_restore', '--exit-on-error', '--no-owner', '--no-acl', '-U', 'carpool_test', '-d', 'carpool_test', '/tmp/production-snapshot.dump') -WindowStyle Hidden -RedirectStandardOutput (Join-Path $privateDirectory 'restore.stdout.log') -RedirectStandardError (Join-Path $privateDirectory 'restore.stderr.log') -Wait -PassThru
if ($restoreProcess.ExitCode -ne 0) { throw 'Restore failed; restricted diagnostics retained without exposing rows.' }
& docker run -d --name carpool-v13-test-redis --network $networkName --network-alias carpool-redis redis:8-alpine redis-server --save '' --appendonly no | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to start isolated empty Redis.' }
[pscustomobject]@{ Status='RESTORED'; Network=$networkName; Internal=$true; DatabaseContainer=$containerName; HostDatabasePorts='none'; Sanitization='pending; no application may connect yet' }
