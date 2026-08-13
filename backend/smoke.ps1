# End-to-end smoke run against a live server. Override with:
#   $env:SMOKE_BASE_URL / $env:SMOKE_PSQL (a psql command line reaching the same DB)
param(
  [string]$BaseUrl = $(if ($env:SMOKE_BASE_URL) { $env:SMOKE_BASE_URL } else { 'http://localhost:8080' }),
  [string]$Psql = $(if ($env:SMOKE_PSQL) { $env:SMOKE_PSQL } else { 'psql -U postgres -d dating_platform' })
)

$ErrorActionPreference = 'Stop'
$base = $BaseUrl
$run = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$e1 = "smoke1-$run@test.dev"
$e2 = "smoke2-$run@test.dev"

function Req($method, $path, $body, $token) {
  $headers = @{}
  if ($token) { $headers['Authorization'] = "Bearer $token" }
  $args = @{ Method = $method; Uri = "$base$path"; Headers = $headers }
  if ($body -ne $null) {
    $args['Body'] = ($body | ConvertTo-Json -Depth 10 -Compress)
    $args['ContentType'] = 'application/json'
  }
  try {
    return Invoke-RestMethod @args
  } catch {
    $resp = $_.Exception.Response
    $code = if ($resp) { [int]$resp.StatusCode } else { 0 }
    $text = ''
    if ($resp) {
      $reader = New-Object System.IO.StreamReader($resp.GetResponseStream())
      $text = $reader.ReadToEnd()
    }
    Write-Host "  !! $method $path -> $code $text" -ForegroundColor Yellow
    return $null
  }
}

function Step($label) { Write-Host "`n=== $label ===" -ForegroundColor Cyan }

Step 'health'
Req GET '/health' $null $null | ConvertTo-Json -Compress

Step 'register (v1) - weak password should 422'
Req POST '/api/v1/auth/register' @{ email=$e1; password='short'; name='Smoke One'; date_of_birth='1995-04-02' } $null

Step 'register (v1) - underage should 422'
Req POST '/api/v1/auth/register' @{ email=$e1; password='password123'; name='Smoke One'; date_of_birth='2015-04-02' } $null

Step 'register (v1) - success'
$a = Req POST '/api/v1/auth/register' @{ email=$e1.ToUpper(); password='password123'; name='Smoke One'; date_of_birth='1995-04-02' } $null
Write-Host "user=$($a.user.email) age=$($a.user.age) expires_in=$($a.expires_in) session=$($a.session_id)"
$t1 = $a.access_token
$r1 = $a.refresh_token

Step 'register duplicate email (case-insensitive) should 409'
Req POST '/api/v1/auth/register' @{ email=$e1; password='password123'; name='Dup'; date_of_birth='1995-04-02' } $null

Step 'me - onboarding flags'
(Req GET '/api/v1/auth/me' $null $t1) | ConvertTo-Json -Depth 5 -Compress

Step 'discover before quiz should 409 assessment_required'
Req GET '/api/v1/discover' $null $t1

Step 'assessment list'
$as = Req GET '/api/v1/personality/assessment' $null $t1
Write-Host "questions=$($as.questions.Count) can_submit=$($as.can_submit) scale=$($as.scale_min)-$($as.scale_max)"

Step 'submit partial answers should 422'
$partial = @{ answers = @(@{ question_id = $as.questions[0].id; value = 4 }) }
Req POST '/api/v1/personality/assessment/submit' $partial $t1

Step 'submit out-of-range value should 422'
$bad = @{ answers = @($as.questions | ForEach-Object { @{ question_id = $_.id; value = 9 } }) }
Req POST '/api/v1/personality/assessment/submit' $bad $t1

Step 'submit full answers'
$ans = @{ answers = @($as.questions | ForEach-Object { @{ question_id = $_.id; value = 4 } }) }
$sub = Req POST '/api/v1/personality/assessment/submit' $ans $t1
Write-Host ($sub | ConvertTo-Json -Depth 5 -Compress)

Step 'retake immediately should 409 retake_too_soon'
Req POST '/api/v1/personality/assessment/submit' $ans $t1

Step 'personality me'
(Req GET '/api/v1/personality/me' $null $t1) | ConvertTo-Json -Depth 5 -Compress

Step 'preferences: invalid age should 422'
Req PUT '/api/v1/preferences' @{ age_min=40; age_max=25; genders=@('woman') } $t1

Step 'preferences: unknown gender should 422'
Req PUT '/api/v1/preferences' @{ age_min=25; age_max=40; genders=@('martian') } $t1

Step 'preferences: bad trait weight should 422'
Req PUT '/api/v1/preferences' @{ age_min=25; age_max=40; genders=@('woman'); trait_weights=@{ openness=99 } } $t1

Step 'preferences: valid'
(Req PUT '/api/v1/preferences' @{ age_min=21; age_max=60; genders=@('man','woman','nonbinary','other'); max_distance_km=50; trait_weights=@{ openness=1.4; neuroticism=0.6 } } $t1) | ConvertTo-Json -Compress

Step 'profile: merge semantics (only bio) then check gender preserved'
(Req PUT '/api/v1/profile' @{ gender='Female'; location='Bangalore'; interests=@('coffee','Coffee','hiking') } $t1) | ConvertTo-Json -Compress
(Req PUT '/api/v1/profile' @{ bio='Testing merge semantics.' } $t1) | ConvertTo-Json -Compress

Step 'profile: reject javascript: photo URL'
Req PUT '/api/v1/profile/photos' @{ photo_urls=@('javascript:alert(1)') } $t1

Step 'profile: set photos'
(Req PUT '/api/v1/profile/photos' @{ photo_urls=@('https://cdn.example.com/a.jpg','https://cdn.example.com/b.jpg') } $t1) | ConvertTo-Json -Compress

Step 'profile: asset_ids should 501 media_unavailable'
Req PUT '/api/v1/profile/photos' @{ asset_ids=@('9f1e9c4e-0000-4000-8000-000000000001') } $t1

Step 'me again - onboarding all true'
(Req GET '/api/v1/auth/me' $null $t1).onboarding | ConvertTo-Json -Compress

Step 'discover page 1'
$d1 = Req GET '/api/v1/discover?limit=5' $null $t1
$d1.items | ForEach-Object { Write-Host ("  {0}  {1,-22} {2}" -f $_.compatibility_score, $_.name, $_.gender) }
Write-Host "next_cursor=$($d1.next_cursor)"
Write-Host "emails present in payload: $(($d1 | ConvertTo-Json -Depth 10) -match 'email')"

Step 'discover page 2 (cursor) - scores must be <= last of page 1'
$d2 = Req GET "/api/v1/discover?limit=5&cursor=$($d1.next_cursor)" $null $t1
$d2.items | ForEach-Object { Write-Host ("  {0}  {1,-22} {2}" -f $_.compatibility_score, $_.name, $_.gender) }

Step 'discover bad cursor should 400'
Req GET '/api/v1/discover?cursor=not-a-cursor' $null $t1

Step 'discover bad limit should 400'
Req GET '/api/v1/discover?limit=9999' $null $t1

Step 'public profile of a candidate (no email)'
$target = $d1.items[0].user_id
$pub = Req GET "/api/v1/users/$target/public" $null $t1
$pub | ConvertTo-Json -Compress

Step 'self-like should 400'
Req POST '/api/v1/likes' @{ user_id=$a.user.id; action='like' } $t1

Step 'bad action should 400'
Req POST '/api/v1/likes' @{ user_id=$target; action='superlike' } $t1

Step 'pass on candidate 2'
$second = $d1.items[1].user_id
(Req POST '/api/v1/likes' @{ user_id=$second; action='pass' } $t1) | ConvertTo-Json -Compress

Step 'duplicate pass is idempotent'
(Req POST '/api/v1/likes' @{ user_id=$second; action='pass' } $t1) | ConvertTo-Json -Compress

Step 'like candidate 1 (no match yet)'
(Req POST '/api/v1/likes' @{ user_id=$target; action='like' } $t1) | ConvertTo-Json -Compress

Step 'passed/liked users no longer in discover'
$d3 = Req GET '/api/v1/discover?limit=50' $null $t1
Write-Host "target still present: $(($d3.items.user_id -contains $target))  second still present: $(($d3.items.user_id -contains $second))"

Step 'second user register + mutual like'
$b = Req POST '/api/v1/auth/register' @{ email=$e2; password='password123'; name='Smoke Two'; date_of_birth='1993-07-11' } $null
$t2 = $b.access_token
$as2 = Req GET '/api/v1/personality/assessment' $null $t2
$ans2 = @{ answers = @($as2.questions | ForEach-Object { @{ question_id = $_.id; value = 3 } }) }
$null = Req POST '/api/v1/personality/assessment/submit' $ans2 $t2
$null = Req PUT '/api/v1/preferences' @{ age_min=18; age_max=99; genders=@('man','woman','nonbinary','other') } $t2
$null = Req PUT '/api/v1/profile' @{ bio='Second smoke user.'; gender='man' } $t2
$null = Req PUT '/api/v1/profile/photos' @{ photo_urls=@('https://cdn.example.com/two.jpg') } $t2

Write-Host '-- user1 likes user2'
(Req POST '/api/v1/likes' @{ user_id=$b.user.id; action='like' } $t1) | ConvertTo-Json -Compress
Write-Host '-- user2 likes user1 (should match)'
$m = Req POST '/api/v1/likes' @{ user_id=$a.user.id; action='like' } $t2
$m | ConvertTo-Json -Compress

Step 'matches list both sides'
(Req GET '/api/v1/matches' $null $t1) | ConvertTo-Json -Depth 6 -Compress
(Req GET '/api/v1/matches' $null $t2) | ConvertTo-Json -Depth 6 -Compress

Step 'public profile now is_matched=true'
(Req GET "/api/v1/users/$($b.user.id)/public" $null $t1).is_matched

Step 'block user2 -> disappears from matches'
Req POST '/api/v1/blocks' @{ user_id=$b.user.id } $t1
(Req GET '/api/v1/matches' $null $t1).items.Count
Step 'blocked user public profile -> 404 for the blocked party'
Req GET "/api/v1/users/$($a.user.id)/public" $null $t2
Step 'blocker sees blocked_by_you on interaction'
Req POST '/api/v1/likes' @{ user_id=$b.user.id; action='like' } $t1
Step 'block list'
(Req GET '/api/v1/blocks' $null $t1) | ConvertTo-Json -Compress
Step 'unblock (idempotent x2)'
Req DELETE "/api/v1/blocks/$($b.user.id)" $null $t1
Req DELETE "/api/v1/blocks/$($b.user.id)" $null $t1
(Req GET '/api/v1/matches' $null $t1).items.Count

Step 'self-block / self-report should 400'
Req POST '/api/v1/blocks' @{ user_id=$a.user.id } $t1
Req POST '/api/v1/reports' @{ user_id=$a.user.id; reason='spam' } $t1

Step 'report: bad reason 422, good reason 202, duplicate collapses'
Req POST '/api/v1/reports' @{ user_id=$b.user.id; reason='because' } $t1
(Req POST '/api/v1/reports' @{ user_id=$b.user.id; reason='harassment'; details='test' } $t1) | ConvertTo-Json -Compress
(Req POST '/api/v1/reports' @{ user_id=$b.user.id; reason='harassment'; details='again' } $t1) | ConvertTo-Json -Compress

Step 'sessions list'
(Req GET '/api/v1/auth/sessions' $null $t1) | ConvertTo-Json -Depth 5 -Compress

Step 'refresh rotates'
$ref = Req POST '/api/v1/auth/refresh' @{ refresh_token=$r1 } $null
Write-Host "new access len=$($ref.access_token.Length) rotated=$($ref.refresh_token -ne $r1)"

Step 'replaying the old refresh token should 401 refresh_token_reused'
Req POST '/api/v1/auth/refresh' @{ refresh_token=$r1 } $null

Step 'rotated token also dead after reuse detection (expect 401)'
Req POST '/api/v1/auth/refresh' @{ refresh_token=$ref.refresh_token } $null

Step 'password reset flow'
Req POST '/api/v1/auth/password/forgot' @{ email='nobody@test.dev' } $null
Req POST '/api/v1/auth/password/reset' @{ token='garbage'; password='password999' } $null
$null = Req POST '/api/v1/auth/password/forgot' @{ email=$e2 } $null
Start-Sleep -Milliseconds 300
# The reset token is only ever delivered through the outbox, so the rest of this
# step needs database access. Skip it rather than fail when psql is unreachable.
$rtok = ''
try {
  $parts = $Psql -split '\s+'
  $query = "select payload->>'reset_token' from outbox_events where event_type='auth.password_reset_requested' order by created_at desc limit 1"
  $tokRow = & $parts[0] @($parts[1..($parts.Count - 1)] + @('-t', '-A', '-c', $query)) 2>$null
  $rtok = ($tokRow | Out-String).Trim()
} catch {
  Write-Host "  !! cannot read the outbox via '$Psql', skipping reset-token asserts" -ForegroundColor Yellow
}

$resetPassword = 'password123'
if ($rtok) {
  Write-Host "reset token from outbox: $($rtok.Length) chars"
  Write-Host '-- reset with short password should 422'
  Req POST '/api/v1/auth/password/reset' @{ token=$rtok; password='abc' } $null
  Write-Host '-- reset with valid password'
  (Req POST '/api/v1/auth/password/reset' @{ token=$rtok; password='newpassword456' } $null) | ConvertTo-Json -Compress
  Write-Host '-- token is single use (expect 400/401)'
  Req POST '/api/v1/auth/password/reset' @{ token=$rtok; password='newpassword789' } $null
  Write-Host '-- old password rejected, new password works'
  Req POST '/api/v1/auth/login' @{ email=$e2; password='password123' } $null
  $resetPassword = 'newpassword456'
  $relog = Req POST '/api/v1/auth/login' @{ email=$e2; password=$resetPassword } $null
  Write-Host "re-login ok: $($relog.access_token.Length -gt 20)"
  Write-Host '-- sessions from before the reset are revoked (expect 401)'
  Req POST '/api/v1/auth/refresh' @{ refresh_token=$b.refresh_token } $null
}

Step 'logout is idempotent and kills the refresh token'
$sess2 = Req POST '/api/v1/auth/login' @{ email=$e2; password=$resetPassword } $null
Req POST '/api/v1/auth/logout' $null $sess2.access_token
Req POST '/api/v1/auth/logout' $null $sess2.access_token
Write-Host '-- refresh after logout (expect 401)'
Req POST '/api/v1/auth/refresh' @{ refresh_token=$sess2.refresh_token } $null

Step 'revoke a specific session then refresh it (expect 401)'
$sess3 = Req POST '/api/v1/auth/login' @{ email=$e2; password=$resetPassword } $null
$sess4 = Req POST '/api/v1/auth/login' @{ email=$e2; password=$resetPassword } $null
Write-Host "sessions listed: $((Req GET '/api/v1/auth/sessions' $null $sess4.access_token).items.Count)"
Req DELETE "/api/v1/auth/sessions/$($sess3.session_id)" $null $sess4.access_token
Req POST '/api/v1/auth/refresh' @{ refresh_token=$sess3.refresh_token } $null
Write-Host '-- revoking an unknown session id (expect 404)'
Req DELETE "/api/v1/auth/sessions/00000000-0000-4000-8000-000000000001" $null $sess4.access_token

Step 'auth failures'
Req GET '/api/v1/auth/me' $null $null
Req GET '/api/v1/auth/me' $null 'not-a-jwt'
Req POST '/api/v1/auth/login' @{ email=$e1; password='wrong-password' } $null
Req POST '/api/v1/auth/login' @{ email='ghost@test.dev'; password='password123' } $null

Step 'unknown route + wrong method'
Req GET '/api/v1/nope' $null $t1
Req DELETE '/api/v1/discover' $null $t1

Step 'legacy endpoints still work and leak no email'
$leg = Req POST '/api/auth/login' @{ email='user1@example.com'; password='password123' } $null
Write-Host "legacy token present: $($leg.token.Length -gt 20)"
$lm = Req GET '/api/matches?limit=3' $null $leg.token
Write-Host "legacy match keys: $(($lm.matches[0].PSObject.Properties.Name) -join ',')"
(Req GET '/api/auth/me' $null $leg.token) | ConvertTo-Json -Depth 4 -Compress
(Req GET '/api/profile' $null $leg.token) | ConvertTo-Json -Compress
(Req PUT '/api/profile' @{ bio='legacy update'; gender='Non-binary'; location='Goa'; photo_url='https://cdn.example.com/legacy.jpg' } $leg.token) | ConvertTo-Json -Compress

Write-Host "`nSMOKE COMPLETE" -ForegroundColor Green
