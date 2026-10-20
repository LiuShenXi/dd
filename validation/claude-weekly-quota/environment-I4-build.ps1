param([string]$Freeze='debug',[switch]$Final)

Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$docker='C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$run=[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$prefix="environment-I4-$Freeze-$run-build"
$beforePath=Join-Path $PSScriptRoot ($prefix+'-source-before.json')
$null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $beforePath
$source=Get-Content -LiteralPath $beforePath -Raw | ConvertFrom-Json
$resources=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'environment-active.json') -Raw | ConvertFrom-Json
$runner=($resources.containers | Where-Object role -eq 'runner').id
$privateRoot=Join-Path $env:LOCALAPPDATA ('Codex/PrivateTests/claude-weekly-quota/I4-build-'+$run)
$context=Join-Path $privateRoot 'candidate'
$providerContext=Join-Path $privateRoot 'provider'
$output='/output/i4-'+$run.ToLowerInvariant()
$tag='sub2api:claude-weekly-i4-candidate-'+$source.source_sha256.Substring(0,12)
$providerSource=Join-Path $PSScriptRoot 'environment-I4-provider/main.go'
$providerHash=(Get-FileHash -LiteralPath $providerSource -Algorithm SHA256).Hash.ToLowerInvariant()
$providerTag='claude-weekly-quota-provider:'+$providerHash.Substring(0,12)
$treeState=if(@(& git -C $repoRoot status --porcelain --untracked-files=normal -- backend frontend).Count -gt 0){'dirty'}else{'clean'}
$revision=$source.head+'-'+$treeState+'-I4-'+$Freeze+'-'+$source.source_sha256
$operations=[Collections.Generic.List[object]]::new()
$result=[ordered]@{stage='I4 isolated candidate';freeze=$Freeze;diagnostic=(-not [bool]$Final);source_sha256=$source.source_sha256;source_dirty=($treeState -eq 'dirty');head=$source.head;frontend_dist_sha256=$source.frontend_dist_sha256;image_tag=$tag;provider_tag=$providerTag;provider_source_sha256=$providerHash;revision=$revision;linux_go='go1.27.2 linux/amd64';cgo_enabled=0;started_utc=[DateTime]::UtcNow.ToString('o');operations=$operations;production_connected=$false;activation_authorized=$false;status='starting'}
function Invoke-Docker([string]$Name,[string[]]$Arguments){
    $log=Join-Path $PSScriptRoot ($prefix+'-'+$Name+'.log')
    & $docker --host npipe:////./pipe/docker_engine @Arguments *> $log
    $code=$LASTEXITCODE
    $operations.Add([pscustomobject]@{name=$Name;arguments=$Arguments;exit_code=$code;log=[IO.Path]::GetFileName($log)})
    if($code -ne 0){throw "I4 Docker operation $Name failed; see $log"}
    return Get-Content -LiteralPath $log -Raw
}
try{
    $provenance=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'coordinator-r0-provenance.json') -Raw|ConvertFrom-Json
    if(-not $provenance.provenance_verified){throw 'A verified R0 provenance receipt is required'}
    $base=[pscustomobject]@{Id=(Invoke-Docker 'r0-before-id' @('image','inspect','--format','{{.Id}}','sub2api:2.8.13-monas-bps.13')).Trim();RootFS=[pscustomobject]@{Layers=@(Invoke-Docker 'r0-before-layers' @('image','inspect','--format','{{json .RootFS.Layers}}','sub2api:2.8.13-monas-bps.13')|ConvertFrom-Json)}}
    if($base.Id -cne $provenance.config_digest_and_classic_import_id -or $base.RootFS.Layers.Count -ne $provenance.rootfs_layer_count){throw 'Local R0 runtime base no longer matches verified provenance'}
    $result.runtime_base_config_id=$base.Id
    $result.runtime_base_oci_index=$provenance.production_oci_index_digest
    $result.runtime_base_provenance='coordinator-r0-provenance.json'
    $result.runtime_base_layers=$base.RootFS.Layers.Count
    $null=New-Item -ItemType Directory -Path $context,$providerContext -Force
    $null=Invoke-Docker 'prepare' @('exec',$runner,'mkdir','-p',$output,'/output/tmp')
    $ldflags='-ldflags=-s -w -X main.Version=2.8.13-monas-claude-weekly-I4-local -X main.Commit='+$revision+' -X main.Date='+[DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')+' -X main.BuildType=release'
    $null=Invoke-Docker 'server' @('exec',$runner,'env','GOMAXPROCS=2','TMPDIR=/output/tmp','GOTMPDIR=/output/tmp','CGO_ENABLED=0','go','build','-p=2','-tags=embed','-trimpath','-buildvcs=false',$ldflags,'-o',($output+'/sub2api'),'./cmd/server')
    $null=Invoke-Docker 'carpool-release' @('exec',$runner,'env','GOMAXPROCS=2','TMPDIR=/output/tmp','GOTMPDIR=/output/tmp','CGO_ENABLED=0','go','build','-p=2','-trimpath','-buildvcs=false','-o',($output+'/carpool-release'),'./cmd/carpool-release')
    $null=Invoke-Docker 'provider-compile' @('exec',$runner,'env','GOMAXPROCS=2','TMPDIR=/output/tmp','GOTMPDIR=/output/tmp','CGO_ENABLED=0','GO111MODULE=off','go','build','-p=2','-trimpath','-o',($output+'/provider'),'/workspace/validation/claude-weekly-quota/environment-I4-provider/main.go')
    foreach($file in @('sub2api','carpool-release')){$null=Invoke-Docker ('copy-'+$file) @('cp',($runner+':'+$output+'/'+$file),(Join-Path $context $file))}
    $null=Invoke-Docker 'copy-provider' @('cp',($runner+':'+$output+'/provider'),(Join-Path $providerContext 'provider'))
    Copy-Item -LiteralPath (Join-Path $repoRoot 'backend/resources') -Destination (Join-Path $context 'resources') -Recurse
    [IO.File]::WriteAllText((Join-Path $context 'docker-entrypoint.sh'),[IO.File]::ReadAllText((Join-Path $repoRoot 'deploy/docker-entrypoint.sh')).Replace("`r`n","`n"),[Text.UTF8Encoding]::new($false))
    $recipe=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'environment-I4-runtime.Dockerfile'))
    [IO.File]::WriteAllText((Join-Path $context 'Dockerfile'),$recipe,[Text.UTF8Encoding]::new($false))
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'environment-I4-provider.Dockerfile') -Destination (Join-Path $providerContext 'Dockerfile')
    $null=Invoke-Docker 'provider-build' @('build','--pull=false','--network=none','--tag',$providerTag,$providerContext)
    $null=Invoke-Docker 'candidate-build' @('build','--pull=false','--tag',$tag,'--build-arg',('SOURCE_SHA256='+$source.source_sha256),'--build-arg',('REVISION='+$revision),'--build-arg',('BUILD_DATE='+[DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')),$context)
    $result.image_id=(Invoke-Docker 'candidate-id' @('image','inspect','--format','{{.Id}}',$tag)).Trim()
    $candidate=[pscustomobject]@{RootFS=[pscustomobject]@{Layers=@(Invoke-Docker 'candidate-layers' @('image','inspect','--format','{{json .RootFS.Layers}}',$tag)|ConvertFrom-Json)}}
    for($layer=0;$layer -lt $base.RootFS.Layers.Count;$layer++){
        if($candidate.RootFS.Layers[$layer] -cne $base.RootFS.Layers[$layer]){throw 'Candidate did not retain the exact verified R0 runtime layers'}
    }
    $baseAfter=(Invoke-Docker 'r0-after' @('image','inspect','--format','{{.Id}}','sub2api:2.8.13-monas-bps.13')).Trim()
    if($baseAfter -cne $base.Id){throw 'R0 base tag changed during the build'}
    $result.runtime_base_layers_retained=$true
    $result.new_layer_count=$candidate.RootFS.Layers.Count-$base.RootFS.Layers.Count
    $result.provider_id=(Invoke-Docker 'provider-id' @('image','inspect','--format','{{.Id}}',$providerTag)).Trim()
    $null=Invoke-Docker 'version' @('run','--rm','--pull','never','--network','none','--read-only','--user','1000:1000','--cap-drop','ALL','--security-opt','no-new-privileges',$tag,'--version')
    $result.runtime_recipe_sha256=(Get-FileHash -LiteralPath (Join-Path $context 'Dockerfile') -Algorithm SHA256).Hash.ToLowerInvariant()
    $afterPath=Join-Path $PSScriptRoot ($prefix+'-source-after.json')
    $null=& (Join-Path $PSScriptRoot 'environment-I1-source-snapshot.ps1') -OutputPath $afterPath
    $after=Get-Content -LiteralPath $afterPath -Raw | ConvertFrom-Json
    $result.source_after_sha256=$after.source_sha256
    $result.source_stable=$source.source_sha256 -ceq $after.source_sha256
    if($Final -and -not $result.source_stable){throw 'Source changed during formal I4 build.'}
    $result.acceptance=([bool]$Final -and $result.source_stable)
    $result.status=if($Final){'pass'}else{'diagnostic_built'}
}catch{$result.status='failed';$result.error=$_.Exception.Message}finally{
    $result.finished_utc=[DateTime]::UtcNow.ToString('o')
    $path=Join-Path $PSScriptRoot ($prefix+'-receipt.json')
    $result | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $path -Encoding utf8
}
[pscustomobject]@{receipt=$path;status=$result.status;image_tag=$tag;provider_tag=$providerTag;source=$source.source_sha256;diagnostic=$result.diagnostic}|ConvertTo-Json
if($result.status -eq 'failed'){Write-Output $result.error;exit 1}
