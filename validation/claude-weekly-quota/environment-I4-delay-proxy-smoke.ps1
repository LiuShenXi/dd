param([Parameter(Mandatory)][string]$EnvironmentReceipt,[Parameter(Mandatory)][string]$ProxyReceipt)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$public=Get-Content -LiteralPath $EnvironmentReceipt -Raw|ConvertFrom-Json
$e=Get-Content -LiteralPath $public.private_fixture_path -Raw|ConvertFrom-Json
$proxy=Get-Content -LiteralPath $ProxyReceipt -Raw|ConvertFrom-Json
if($proxy.target -cne $e.URL -or -not $proxy.loopback_only){throw 'Proxy target must be the exact independent synthetic browser candidate'}
$login=Invoke-RestMethod -Method Post -Uri ($e.URL+'/api/v1/auth/login') -ContentType 'application/json' -Body (@{email=$e.AdminEmail;password=$e.Password}|ConvertTo-Json -Compress) -TimeoutSec 8
$client=[Net.Http.HttpClient]::new();$client.Timeout=[TimeSpan]::FromSeconds(10)
$client.DefaultRequestHeaders.Authorization=[Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer',$login.data.access_token)
$aPath='/api/v1/admin/users/'+$e.A+'/subscription-quota'
$bPath='/api/v1/admin/users/'+$e.B+'/subscription-quota'
$outcomes=@()
$initialState=Invoke-RestMethod -Uri ($proxy.url+'/__cwq/control') -TimeoutSec 5
$initialHeld=$initialState.held_responses
try{
 foreach($fault in @(0,503)){
  $arm=@{path=$aPath;method='GET';hold=$true;error_status=$fault}|ConvertTo-Json -Compress
  $null=Invoke-RestMethod -Method Post -Uri ($proxy.url+'/__cwq/control') -ContentType 'application/json' -Body $arm -TimeoutSec 5
  $pending=$client.GetAsync($proxy.url+$aPath+'?group_id='+$e.Group)
  $deadline=[DateTime]::UtcNow.AddSeconds(5)
  do{$state=Invoke-RestMethod -Uri ($proxy.url+'/__cwq/control') -TimeoutSec 5;if($state.held_responses -gt ($initialHeld+$outcomes.Count)){break};Start-Sleep -Milliseconds 100}while([DateTime]::UtcNow -lt $deadline)
  if($pending.IsCompleted -or $state.held_responses -ne ($initialHeld+$outcomes.Count+1)){throw 'Exact A backend response was not held'}
  $bResponse=$client.GetAsync($proxy.url+$bPath+'?group_id='+$e.Group).GetAwaiter().GetResult()
  if([int]$bResponse.StatusCode -ne 200){throw 'B summary must pass while A is held'}
  $bBody=$bResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult()|ConvertFrom-Json
  if($bBody.data.current_term.id -ne $e.BTerm){throw 'Actual B summary card identity changed'}
  $null=Invoke-RestMethod -Method Post -Uri ($proxy.url+'/__cwq/control') -ContentType 'application/json' -Body '{"release":true}' -TimeoutSec 5
  $aResponse=$pending.GetAwaiter().GetResult();$aBody=$aResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult()|ConvertFrom-Json
  if($fault -eq 0){if([int]$aResponse.StatusCode -ne 200 -or $aBody.data.current_term.id -ne $e.ATerm){throw 'Released authentic A summary card identity changed'}}else{if([int]$aResponse.StatusCode -ne 503 -or $aBody.code -cne 'SYNTHETIC_PROXY_DELAYED_ERROR'){throw 'Synthetic delayed fault not explicit'}}
  $outcomes+=@{A_held_until_release=$true;B_passed_with_actual_identity=$true;released_A_status=[int]$aResponse.StatusCode;synthetic_error=($fault -ne 0)}
 }
}finally{
 $null=Invoke-RestMethod -Method Post -Uri ($proxy.url+'/__cwq/control') -ContentType 'application/json' -Body '{"release":true}' -TimeoutSec 5
 $client.Dispose()
}
$state=Invoke-RestMethod -Uri ($proxy.url+'/__cwq/control') -TimeoutSec 5
$result=[ordered]@{stage='I4 task-only delay proxy functional smoke';synthetic_only=$true;environment=[IO.Path]::GetFileName($EnvironmentReceipt);proxy=[IO.Path]::GetFileName($ProxyReceipt);outcomes=$outcomes;state=$state;browser_UI_assertions_executed=$false;business_server_modified=$false;observed_utc=[DateTime]::UtcNow.ToString('o')}
$path=Join-Path $PSScriptRoot ('I4-delay-proxy-smoke-'+[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')+'.json')
$result|ConvertTo-Json -Depth 7|Set-Content -LiteralPath $path -Encoding utf8
[pscustomobject]@{receipt=$path;outcomes=$outcomes;browser_UI_assertions_executed=$false}|ConvertTo-Json -Depth 5
