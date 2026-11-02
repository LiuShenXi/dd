param([Parameter(Mandatory)][ValidateSet('arm','release','state','disarm')][string]$Action,[string]$RuleID='', [ValidateSet('APreview','BPreview','CTerms')][string]$Target='APreview',[ValidateSet('before_upstream','after_backend_response')][string]$Phase='after_backend_response',[ValidateSet('hold','drop','reject')][string]$Effect='hold',[ValidateSet(0,409,503)][int]$Status=0)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$url='http://127.0.0.1:54551'
$body=@{action=$Action;rule_id=$RuleID}
if($Action -eq 'arm'){
 $body.path=switch($Target){APreview{'/api/v1/admin/users/2/subscription-quota/terms/preview'}BPreview{'/api/v1/admin/users/3/subscription-quota/terms/preview'}CTerms{'/api/v1/admin/users/4/subscription-quota/terms'}}
 $body.method='POST';$body.phase=$Phase;$body.effect=$Effect
}
if($Status){$body.status=$Status}
if($Action -eq 'state'){$result=Invoke-RestMethod -Uri ($url+'/__cwq/control') -TimeoutSec 5}else{$result=Invoke-RestMethod -Method Post -Uri ($url+'/__cwq/control') -ContentType 'application/json' -Body ($body|ConvertTo-Json -Compress) -TimeoutSec 5}
[ordered]@{time=[DateTime]::UtcNow.ToString('o');proxy=$url;control=$body;result=$result;business_API_called=$false;browser_controlled=$false}|ConvertTo-Json -Compress -Depth 12|Add-Content -LiteralPath (Join-Path $PSScriptRoot 'I4-ui-proxy-control-events.jsonl') -Encoding utf8
$result|ConvertTo-Json -Depth 10
