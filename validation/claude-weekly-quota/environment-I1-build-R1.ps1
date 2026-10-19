param()

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$active=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-I1-final-active.json') -Raw | ConvertFrom-Json
$source=Get-Content -LiteralPath $active.source_manifest -Raw | ConvertFrom-Json
$resources=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-active.json') -Raw | ConvertFrom-Json
$runner=($resources.containers | Where-Object role -eq 'runner').id
$docker='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$prefix="environment-I1-$($active.freeze)-$($active.run_id)-build"
$receiptPath=Join-Path $PSScriptRoot ($prefix+'-receipt.json')
$version='2.8.13-monas-claude-weekly-i1-r1-local'
$revision=$source.head+'-dirty-frozen-I1-'+$active.source_sha256
$tag='sub2api:claude-weekly-i1-r1-'+$active.source_sha256.Substring(0,12)
$privateRoot=Join-Path $env:LOCALAPPDATA ('Codex/PrivateTests/claude-weekly-quota/I1-'+$active.run_id)
$context=Join-Path $privateRoot 'image-context'
$containerOutput='/output/i1-'+$active.run_id.ToLowerInvariant()
$started=[DateTime]::UtcNow
$buildDate=$started.ToString('yyyy-MM-ddTHH:mm:ssZ')
$operations=[Collections.Generic.List[object]]::new()
$result=[ordered]@{freeze=$active.freeze;stage='I1 preliminary R1';source_sha256=$active.source_sha256;frontend_dist_sha256=$active.frontend_dist_sha256;source_manifest=[IO.Path]::GetFileName($active.source_manifest);revision=$revision;version=$version;image_tag=$tag;started_utc=$started.ToString('o');image_built=$false;production_enabled=$false;activation_supported=$false;requires_I3_recovery_refreeze=$true;linux_go='go1.27.2 linux/amd64';cgo_enabled=0;runtime_recipe='environment-I1-R1.Dockerfile';private_build_context=$context;operations=$operations;status='starting'}

function Invoke-RecordedDocker([string]$Name,[string[]]$Arguments) {
    $log=Join-Path $PSScriptRoot ($prefix+'-'+$Name+'.log')
    $operation=[ordered]@{name=$Name;executable=$docker;arguments=@('--host','npipe:////./pipe/docker_engine')+$Arguments;started_utc=[DateTime]::UtcNow.ToString('o');log=[IO.Path]::GetFileName($log)}
    & $docker --host npipe:////./pipe/docker_engine @Arguments *> $log
    $code=$LASTEXITCODE
    $operation.exit_code=$code
    $operation.finished_utc=[DateTime]::UtcNow.ToString('o')
    $operations.Add([pscustomobject]$operation)
    if($code -ne 0){throw "Docker operation $Name failed ($code); inspect $log"}
    return (Get-Content -LiteralPath $log -Raw)
}

try {
    foreach($phase in @('pg','race')) {
        $path=Join-Path $PSScriptRoot ("environment-I1-$($active.freeze)-$($active.run_id)-$phase-receipt.json")
        $check=Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
        if(-not $check.acceptance -or $check.source_sha256 -ne $active.source_sha256){throw "Final $phase validation has not passed for this frozen source."}
    }
    if(Test-Path -LiteralPath $context){throw 'A build context already exists for this run; preserve and inspect it before retrying.'}
    $null=New-Item -ItemType Directory -Path $context -Force
    $null=Invoke-RecordedDocker 'prepare-output' @('exec',$runner,'mkdir','-p',$containerOutput,'/output/tmp')
    $ldflags='-ldflags=-s -w -X main.Version='+$version+' -X main.Commit='+$revision+' -X main.Date='+$buildDate+' -X main.BuildType=release'
    $null=Invoke-RecordedDocker 'go-server' @('exec',$runner,'env','GOMAXPROCS=2','TMPDIR=/output/tmp','GOTMPDIR=/output/tmp','CGO_ENABLED=0','GOOS=linux','GOARCH=amd64','go','build','-p=2','-tags=embed','-trimpath','-buildvcs=false',$ldflags,'-o',($containerOutput+'/sub2api'),'./cmd/server')
    $null=Invoke-RecordedDocker 'go-carpool-release' @('exec',$runner,'env','GOMAXPROCS=2','TMPDIR=/output/tmp','GOTMPDIR=/output/tmp','CGO_ENABLED=0','GOOS=linux','GOARCH=amd64','go','build','-p=2','-trimpath','-buildvcs=false','-ldflags=-s -w','-o',($containerOutput+'/carpool-release'),'./cmd/carpool-release')
    $null=Invoke-RecordedDocker 'copy-server' @('cp',($runner+':'+$containerOutput+'/sub2api'),(Join-Path $context 'sub2api'))
    $null=Invoke-RecordedDocker 'copy-carpool-release' @('cp',($runner+':'+$containerOutput+'/carpool-release'),(Join-Path $context 'carpool-release'))
    Copy-Item -LiteralPath (Join-Path $repoRoot 'backend/resources') -Destination (Join-Path $context 'resources') -Recurse
    $entrypoint=[IO.File]::ReadAllText((Join-Path $repoRoot 'deploy/docker-entrypoint.sh')).Replace("`r`n","`n")
    [IO.File]::WriteAllText((Join-Path $context 'docker-entrypoint.sh'),$entrypoint,[Text.UTF8Encoding]::new($false))
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'environment-I1-R1.Dockerfile') -Destination (Join-Path $context 'Dockerfile')
    $result.runtime_recipe_sha256=(Get-FileHash -LiteralPath (Join-Path $context 'Dockerfile') -Algorithm SHA256).Hash.ToLowerInvariant()
    $result.artifacts=@('sub2api','carpool-release' | ForEach-Object {
        $file=Get-Item -LiteralPath (Join-Path $context $_)
        [pscustomobject]@{name=$_;path=$file.FullName;bytes=$file.Length;sha256=(Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}
    })
    $null=Invoke-RecordedDocker 'docker-build' @('build','--pull=false','--file',(Join-Path $context 'Dockerfile'),'--tag',$tag,'--build-arg',('SOURCE_SHA256='+$active.source_sha256),'--build-arg',('REVISION='+$revision),'--build-arg',('BUILD_DATE='+$buildDate),$context)
    $details=@((Invoke-RecordedDocker 'image-inspect' @('image','inspect',$tag)) | ConvertFrom-Json)[0]
    $result.image_built=$true
    $result.image_id=$details.Id
    $result.image_size_bytes=$details.Size
    $result.image_os=$details.Os
    $result.image_architecture=$details.Architecture
    $result.image_labels=$details.Config.Labels
    if($details.Config.Labels.'com.monas.source-sha256' -ne $active.source_sha256 -or $details.Config.Labels.'org.opencontainers.image.revision' -ne $revision){throw 'The image labels do not bind this frozen dirty source.'}
    $versionText=Invoke-RecordedDocker 'version-smoke' @('run','--rm','--pull','never','--network','none','--read-only','--user','1000:1000','--cap-drop','ALL','--security-opt','no-new-privileges',$tag,'--version')
    if(-not $versionText.Contains($revision) -or -not $versionText.Contains($version)){throw 'The image version output does not bind the frozen source.'}
    $hashes=Invoke-RecordedDocker 'image-binary-hashes' @('run','--rm','--pull','never','--network','none','--read-only','--user','1000:1000','--cap-drop','ALL','--security-opt','no-new-privileges','--entrypoint','sha256sum',$tag,'/app/sub2api','/app/carpool-release')
    foreach($artifact in $result.artifacts){if(-not $hashes.Contains($artifact.sha256)){throw 'An image binary differs from the recorded build artifact.'}}
    $null=Invoke-RecordedDocker 'pg-client-version' @('run','--rm','--pull','never','--network','none','--read-only','--user','1000:1000','--cap-drop','ALL','--security-opt','no-new-privileges','--entrypoint','pg_dump',$tag,'--version')
    $afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
    $null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
    $after=Get-Content -LiteralPath $afterPath -Raw | ConvertFrom-Json
    $result.source_after_sha256=$after.source_sha256
    $result.source_stable=$active.source_sha256 -ceq $after.source_sha256
    if(-not $result.source_stable){throw 'Source changed during the image build; this image is not the frozen candidate.'}
    $result.status='pass'
} catch {
    $result.status='failed'
    $result.error=$_.Exception.Message
} finally {
    $result.finished_utc=[DateTime]::UtcNow.ToString('o')
    $result | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $receiptPath -Encoding utf8
}
[pscustomobject]@{status=$result.status;image_tag=$tag;source_sha256=$active.source_sha256;receipt=$receiptPath;activation_supported=$false} | ConvertTo-Json
if($result.status -ne 'pass'){Write-Output $result.error;exit 1}
