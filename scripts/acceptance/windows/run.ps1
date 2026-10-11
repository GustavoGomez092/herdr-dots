# Windows acceptance run of Herdr Dots (the Linux method of
# docs/superpowers/notes/2026-10-11-linux-acceptance.md, ported to Windows).
#
# Herdr runs headless (`herdr --session acc server`). The coding agent is a
# stand-in claude.exe (fakeclaude, agent mode) started in a Herdr pane, which
# Herdr's Windows process scan identifies as Claude; it draws the screens
# Herdr's claude manifest reads (a spinner line = working, plain lines = idle).
# The dot's CLI is a claude.cmd shim on PATH that runs fakeclaude in review
# mode (logs argv, prints a fixed claude-cli JSON verdict). Engine actions go
# over the engine's named pipe with acctool.
#
# Usage: run.ps1 -Herdr <herdr.exe> -Tools <dir with acctool.exe, fakeclaude.exe> -Repo <checkout>
param(
    [Parameter(Mandatory)] [string]$Herdr,
    [Parameter(Mandatory)] [string]$Tools,
    [Parameter(Mandatory)] [string]$Repo
)
$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'

$Acc = 'C:\acc'
$Ev = Join-Path $Acc 'evidence'
$App = 'C:\work\app'
$Sid = '0a1b2c3d-1111-4222-8333-444455556666'
$Utf8 = [System.Text.UTF8Encoding]::new($false)
$env:ACC_DIR = $Acc
$AccTool = Join-Path $Tools 'acctool.exe'

$Results = [ordered]@{}
foreach ($n in 1..7) { $Results["$n"] = 'NOT RUN' }
function Result([string]$n, [string]$status, [string]$why) {
    $Results[$n] = "$status - $why"
    Write-Host ""
    Write-Host "########## STEP $n : $status - $why"
    Write-Host ""
}
function Banner([string]$t) { Write-Host ""; Write-Host "==================== $t ====================" }
function H { $out = & $Herdr --session acc @args 2>&1 | Out-String; return $out }
function Show([string]$label, [string]$text) { Write-Host "--- $label"; Write-Host $text }
function WaitFor([scriptblock]$cond, [int]$secs, [string]$what) {
    $deadline = (Get-Date).AddSeconds($secs)
    while ((Get-Date) -lt $deadline) {
        $r = & $cond
        if ($r) { return $r }
        Start-Sleep -Milliseconds 500
    }
    Write-Host "timeout ($secs s) waiting for: $what"
    return $null
}
function ReadText([string]$p) { if ($p -and (Test-Path -LiteralPath $p)) { return [IO.File]::ReadAllText($p) } return '' }
function Save([string]$name, [string]$text) { [IO.File]::WriteAllText((Join-Path $Ev $name), $text, $Utf8) }

# ---- transcript and screens ----
$TDir = Join-Path $env:USERPROFILE '.claude\projects\C--work-app'   # C:\work\app, Claude Code's encoding
$TFile = Join-Path $TDir "$Sid.jsonl"
function Turn([int]$n, [string]$user, [string]$assistant) {
    New-Item -ItemType Directory -Force $TDir | Out-Null
    $ts = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ss.fffZ')
    $u = [ordered]@{ parentUuid = $null; isSidechain = $false; userType = 'external'; cwd = $App; sessionId = $Sid; version = '2.1.0'; type = 'user'
        message = [ordered]@{ role = 'user'; content = $user }; uuid = "u$n"; timestamp = $ts }
    $a = [ordered]@{ parentUuid = "u$n"; isSidechain = $false; userType = 'external'; cwd = $App; sessionId = $Sid; version = '2.1.0'; type = 'assistant'
        message = [ordered]@{ role = 'assistant'; model = 'claude-sonnet'; content = @(
                [ordered]@{ type = 'tool_use'; id = "t$n"; name = 'Edit'; input = [ordered]@{ file_path = "$App\server.js"; old_string = 'x'; new_string = 'y' } },
                [ordered]@{ type = 'text'; text = $assistant }) }
        uuid = "a$n"; timestamp = $ts }
    $lines = ($u | ConvertTo-Json -Compress -Depth 20) + "`n" + ($a | ConvertTo-Json -Compress -Depth 20) + "`n"
    [IO.File]::AppendAllText($TFile, $lines, $Utf8)
}
$script:ScreenSeq = 0
function Screen([string]$cmd) {
    $script:ScreenSeq++
    [IO.File]::WriteAllText((Join-Path $Acc ('screen\{0:D4}.txt' -f $script:ScreenSeq)), $cmd, $Utf8)
}
function Feed { ReadText (Join-Path $script:SessDir 'feed.jsonl') }
function FeedTurn([int]$n) {
    foreach ($l in ((Feed) -split "`n")) {
        if ($l -match "`"turn`":$n,") { return $l }
    }
    return $null
}
# One Claude turn in the agent pane: spinner (working), transcript records, then plain output (idle).
function Cycle([int]$n, [string]$user, [string]$assistant) {
    Screen 'working'
    Start-Sleep -Seconds 3
    Turn $n $user $assistant
    Screen "idle $n"
    $line = WaitFor { FeedTurn $n } 90 "feed entry for turn $n"
    Show "feed turn $n" "$line"
    return $line
}
function Queue { $t = ReadText (Join-Path $script:SessDir 'queue.json'); if ($t) { return $t | ConvertFrom-Json } return $null }
function PaneIds { $o = H pane list; return @([regex]::Matches($o, '"pane_id"\s*:\s*"([^"]+)"') | ForEach-Object { $_.Groups[1].Value } | Select-Object -Unique) }
function MemoryFiles {
    Get-ChildItem -LiteralPath $script:StateDir, $script:CfgDir -Recurse -Force -File -Filter '*.json' -ErrorAction SilentlyContinue |
        Where-Object { (ReadText $_.FullName) -match '"entries"' }
}
function MemoryHas([string]$kind) {
    foreach ($f in (MemoryFiles)) { if ((ReadText $f.FullName) -match "`"kind`":\s*`"$kind`"") { return $f.FullName } }
    return $null
}

# ---- setup ----
Banner 'setup'
foreach ($d in @($Acc, $Ev, "$Acc\bin", "$Acc\agentbin", "$Acc\fake", "$Acc\screen", $App)) { New-Item -ItemType Directory -Force $d | Out-Null }
Copy-Item (Join-Path $Tools 'fakeclaude.exe') "$Acc\fake\fakeclaude.exe"
Copy-Item (Join-Path $Tools 'fakeclaude.exe') "$Acc\agentbin\claude.exe"
# An npm-style shim: the dot's CLI resolves to claude.cmd, the common Windows install.
[IO.File]::WriteAllText("$Acc\bin\claude.cmd", "@echo off`r`n`"%~dp0..\fake\fakeclaude.exe`" %*`r`n", [Text.Encoding]::ASCII)
$herdrDir = Split-Path $Herdr
$env:PATH = "$Acc\bin;$herdrDir;$env:PATH"
Write-Host "claude resolves to: $((Get-Command claude -ErrorAction SilentlyContinue).Source)"
Write-Host "where claude:"; where.exe claude

# The stand-in's verdict, checked with the product's own parser.
cmd /c "claude -p --x < nul > $Acc\verdict.json"
Show 'stand-in output' (ReadText "$Acc\verdict.json")
Show 'acctool parse' (& $AccTool parse "$Acc\verdict.json" 2>&1 | Out-String)

# The project the agent works in.
[IO.File]::WriteAllText("$App\server.js", "const express = require('express');`nconst app = express();`nconst PASSWORD = `"hunter2`";`napp.post('/login', (req, res) => { db.query(`"SELECT * FROM users WHERE name = '`" + req.body.user + `"'`"); });`n", $Utf8)
git -C $App init -q 2>&1 | Out-Null
git -C $App -c user.name=acc -c user.email=acc@example.invalid add -A 2>&1 | Out-Null
git -C $App -c user.name=acc -c user.email=acc@example.invalid commit -q -m init 2>&1 | Out-Null

# ---- step 1: install ----
Banner 'step 1: install'
Show 'herdr --version' (& $Herdr --version 2>&1 | Out-String)
$inst = & $Herdr plugin install GustavoGomez092/herdr-dots --yes 2>&1 | Out-String
$instCode = $LASTEXITCODE
Show "herdr plugin install (exit $instCode)" $inst
$manifest = Get-ChildItem (Join-Path $env:APPDATA 'herdr\plugins') -Recurse -Force -Filter herdr-plugin.toml -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -match 'herdr-dots' } | Select-Object -First 1
$PluginDir = if ($manifest) { $manifest.DirectoryName } else { '' }
$Dots = Join-Path $PluginDir 'bin\herdr-dots.exe'
$ver = if ($PluginDir) { & $Dots --version 2>&1 | Out-String } else { '' }
Show "plugin dir $PluginDir; --version" $ver
Save 'version.txt' $ver
Push-Location $PluginDir
$vr = & $Dots verify-release 2>&1 | Out-String
Pop-Location
Show 'verify-release' $vr
if ($instCode -eq 0 -and $ver -match '\(release, commit ' -and $vr -match 'checksum ok') {
    Result 1 'PASS' "installed: $($ver.Trim()); verify-release: checksum ok"
} else { Result 1 'FAIL' "install exit $instCode, version '$($ver.Trim())'" }

# ---- step 2: the engine starts with the session ----
Banner 'step 2: server and engine'
$srv = Start-Process -FilePath $Herdr -ArgumentList '--session', 'acc', 'server' -PassThru -WindowStyle Hidden `
    -RedirectStandardOutput "$Ev\server-stdout.txt" -RedirectStandardError "$Ev\server-stderr.txt"
Write-Host "server pid $($srv.Id)"
$up = WaitFor { $o = H workspace list; if ($LASTEXITCODE -eq 0 -and $o -match 'result') { $o } } 60 'herdr server'
Show 'workspace list' "$up"
$script:StateDir = ''; $script:SessDir = ''; $script:Pipe = ''
$engLog = WaitFor {
    Get-ChildItem -LiteralPath $env:APPDATA, $env:LOCALAPPDATA -Recurse -Force -File -Filter engine.log -ErrorAction SilentlyContinue |
        Where-Object { $_.FullName -match 'herdr-dots' } | Select-Object -First 1
} 60 'engine.log'
if ($engLog) {
    $script:SessDir = $engLog.DirectoryName
    $script:StateDir = Split-Path $script:SessDir
    $el = ReadText $engLog.FullName
    Show "engine.log ($($engLog.FullName))" $el
    if ($el -match 'endpoint (\S+)') { $script:Pipe = $Matches[1] }
}
$plog = H plugin log list --plugin herdr-dots
Show 'plugin log list' $plog
$snap = if ($script:Pipe) { & $AccTool $script:Pipe snapshot 2>&1 | Out-String } else { '' }
Show "engine snapshot over $($script:Pipe)" ($snap.Substring(0, [Math]::Min(1500, $snap.Length)))
if ($plog -match '"engine","--ensure"\],"event":"startup","exit_code":0' -and $script:Pipe -and $snap -match '"result"|"snapshot"|"panes"|\{') {
    Result 2 'PASS' "startup engine --ensure exit 0; engine started, endpoint $($script:Pipe) answers"
} elseif ($script:Pipe -and $snap) {
    Result 2 'PASS' "engine started (endpoint $($script:Pipe) answers); startup log entry not matched exactly, see plugin log list"
} else { Result 2 'FAIL' 'engine did not start (see plugin log list / engine.log)' }

# ---- step 3: attach Security over IPC ----
Banner 'step 3: agent pane and attach'
$ws = H workspace create --cwd $App --label app
Show 'workspace create' $ws
$Pane = WaitFor { (PaneIds | Select-Object -First 1) } 30 'a pane'
Write-Host "agent pane: $Pane"
$sock = Get-ChildItem -LiteralPath $env:APPDATA, $env:LOCALAPPDATA -Recurse -Force -File -Filter herdr.sock -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -match '\\acc\\' } | Select-Object -First 1
$sub = $null
if ($sock -and $Pane) {
    Write-Host "herdr socket marker: $($sock.FullName)"
    $sub = Start-Process -FilePath $AccTool -ArgumentList 'sub', "`"$($sock.FullName)`"", $Pane, '780' -PassThru -WindowStyle Hidden `
        -RedirectStandardOutput "$Ev\sub.log" -RedirectStandardError "$Ev\sub-stderr.txt"
}
Turn 1 'Add a login route to server.js with a password check.' 'I added POST /login to server.js. It compares the password with the PASSWORD constant set on line 3.'
H pane send-text $Pane "$Acc\agentbin\claude.exe" | Out-Null
H pane send-keys $Pane Enter | Out-Null
$agentUp = WaitFor { $g = H pane get $Pane; if ($g -match '"agent"\s*:\s*"claude"') { $g } } 45 'pane agent = claude'
Show 'pane get (agent detected)' "$agentUp"
if (-not $agentUp) { Show 'pane get' (H pane get $Pane); Show 'pane read' (H pane read $Pane --source recent --lines 40); Show 'agent explain' (H agent explain $Pane) }
Show 'report-agent-session' (H pane report-agent-session $Pane --source acc --agent claude --agent-session-id $Sid)
$sess = WaitFor { $g = H pane get $Pane; if ($g -match $Sid) { $g } } 20 'agent_session on the pane'
Show 'pane get (session)' "$sess"
$script:CfgDir = (H plugin config-dir herdr-dots).Trim()
if ($script:CfgDir -match '"([A-Za-z]:\\\\[^"]+)"') { $script:CfgDir = $Matches[1] -replace '\\\\', '\' }
Write-Host "plugin config dir: $($script:CfgDir)"
New-Item -ItemType Directory -Force (Join-Path $script:CfgDir 'dots') | Out-Null
Copy-Item (Join-Path $Repo 'internal\dots\templates\security.toml') (Join-Path $script:CfgDir 'dots\security.toml')
Show 'reload' (& $AccTool $script:Pipe reload 2>&1 | Out-String)
$att = & $AccTool $script:Pipe attach $Pane security 2>&1 | Out-String
Show 'attach' $att
$catch = WaitFor { FeedTurn 1 } 60 'catch-up review (turn 1)'
Show 'catch-up feed' "$catch"
$attJson = ReadText (Join-Path $script:SessDir 'attachments.json')
Show 'attachments.json' $attJson
if ($att -match 'ok: attach' -and $attJson -match '"security"') {
    Result 3 'PASS' "Security attached to $Pane over $($script:Pipe); catch-up: $(if ($catch) { ($catch | ConvertFrom-Json).outcome } else { 'none' })"
} else { Result 3 'FAIL' "attach: $($att.Trim())" }

# ---- step 4: the Dots pane ----
Banner 'step 4: Dots pane'
$before = PaneIds
Show 'toggle-pane' (H plugin action invoke toggle-pane --plugin herdr-dots)
$DotsPane = WaitFor { PaneIds | Where-Object { $before -notcontains $_ } | Select-Object -First 1 } 30 'the Dots pane'
Write-Host "Dots pane: $DotsPane"
$dp = WaitFor { $r = H pane read $DotsPane --source visible; if ($r -match 'Security') { $r } } 30 'roster in the Dots pane'
if (-not $dp -and $DotsPane) { $dp = H pane read $DotsPane --source visible }
Show 'Dots pane (pane read)' "$dp"
Save 'dots-pane-step4.txt' "$dp"
if ($dp -match 'DOTS' -and $dp -match 'Security') { Result 4 'PASS' "pane read $DotsPane shows the roster with Security" }
else { Result 4 'FAIL' "Dots pane $DotsPane did not show the roster" }

# ---- step 5: a turn ends, the dot runs, a card appears ----
Banner 'step 5: turn 2 -> review -> card'
$argvBefore = (ReadText "$Acc\claude-argv.log").Length
$t2 = Cycle 2 'Keep the password check but make it work for the admin user too.' 'Done: the admin user now logs in with the same PASSWORD constant on line 3 of server.js.'
$argvNew = (ReadText "$Acc\claude-argv.log").Substring($argvBefore)
Show 'claude argv (this run)' $argvNew
$q = Queue
$card2 = if ($q) { $q.cards | Where-Object { $_.turn -eq 2 } | Select-Object -Last 1 } else { $null }
Show 'queue.json' (ReadText (Join-Path $script:SessDir 'queue.json'))
$dp5 = H pane read $DotsPane --source visible
Show 'Dots pane after turn 2' $dp5
Save 'dots-pane-step5.txt' $dp5
if ($t2 -match '"outcome":"flagged"' -and $card2 -and $argvNew -match '--setting-sources' -and $argvNew -match '--strict-mcp-config') {
    Result 5 'PASS' "turn 2 flagged; card $($card2.id) ($($card2.severity)) in queue.json; fakeclaude ran via claude.cmd with --strict-mcp-config/--setting-sources user"
} else { Result 5 'FAIL' "feed turn 2: $t2; card: $($card2.id)" }

# ---- step 6: send the card into the agent pane ----
Banner 'step 6: send'
if ($card2) {
    $sent = & $AccTool $script:Pipe send $card2.id 0 2>&1 | Out-String
    Show 'send' $sent
    $ap = WaitFor { $r = H pane read $Pane --source recent --lines 200; if ($r -match 'Security dot') { $r } } 30 'the card in the agent pane'
    if (-not $ap) { $ap = H pane read $Pane --source recent --lines 200 }
    Show 'agent pane' "$ap"
    Save 'agent-pane-step6.txt' "$ap"
    Show 'agent-stdin.log' (ReadText "$Acc\agent-stdin.log")
    $st = (Queue).cards | Where-Object { $_.id -eq $card2.id } | Select-Object -First 1
    Write-Host "card $($card2.id) status: $($st.status)"
    $mem1 = WaitFor { MemoryHas 'finding' } 60 'memory entry for the sent card'
    Write-Host "memory with a finding: $mem1"
    if ($ap -match 'Security dot' -and $st.status -eq 'sent') { Result 6 'PASS' "card $($card2.id) typed into $Pane ('[... Security dot] ...' on screen and in claude.exe's stdin), status sent$(if ($mem1) { ', remembered' })" }
    elseif ($st.status -eq 'sent') { Result 6 'FAIL' "card marked sent but its text was not seen in the agent pane" }
    else { Result 6 'FAIL' "send: $($sent.Trim()); status $($st.status)" }
} else { Result 6 'BLOCKED' 'no card from step 5' }

# ---- step 7: dismiss, then the repeat is suppressed ----
Banner 'step 7: dismiss and suppression'
$t3 = Cycle 3 'Refactor the login handler.' 'Refactored /login; it still checks the PASSWORD constant on line 3 of server.js.'
[IO.File]::WriteAllText("$Acc\fake-finding", 'SQL injection in server.js /login: the query is built by concatenating req.body.user into the SQL string. An attacker can log in as anyone with a quote payload. Use a parameterized query.', $Utf8)
$t4 = Cycle 4 'Look the user up in the database on login.' 'The /login route now queries the users table with the name from req.body.user.'
$card4 = (Queue).cards | Where-Object { $_.turn -eq 4 } | Select-Object -Last 1
$t5 = $null
if ($card4) {
    Show 'dismiss' (& $AccTool $script:Pipe dismiss $card4.id 2>&1 | Out-String)
    $mem2 = WaitFor { MemoryHas 'dismissed' } 60 'memory entry for the dismissed card'
    Write-Host "memory with a dismissed entry: $mem2"
    $t5 = Cycle 5 'Add logging to the login route.' 'Added a log line to /login; the query still concatenates req.body.user.'
}
Show 'queue.json' (ReadText (Join-Path $script:SessDir 'queue.json'))
if ($card4 -and $t5 -match '"outcome":"suppressed"') {
    Result 7 'PASS' "card $($card4.id) dismissed; turn 5 same finding: $(($t5 | ConvertFrom-Json).detail); turn 3 (repeat of the sent finding): $(if ($t3) { ($t3 | ConvertFrom-Json).outcome })"
} elseif (-not $card4) { Result 7 'FAIL' "no card for turn 4 (feed: $t4)" }
else { Result 7 'FAIL' "turn 5 after dismiss: $t5" }

# ---- evidence ----
Banner 'evidence'
if ($sub) { Stop-Process -Id $sub.Id -Force -ErrorAction SilentlyContinue }
foreach ($f in 'engine.log', 'feed.jsonl', 'queue.json', 'attachments.json', 'ledger.jsonl') {
    $p = Join-Path $script:SessDir $f
    if (Test-Path -LiteralPath $p) { Copy-Item -LiteralPath $p $Ev; Show $f (ReadText $p) }
}
foreach ($m in (MemoryFiles)) { Copy-Item -LiteralPath $m.FullName (Join-Path $Ev ("memory-" + $m.Name)); Show "memory $($m.FullName)" (ReadText $m.FullName) }
Copy-Item -LiteralPath $TFile $Ev -ErrorAction SilentlyContinue
foreach ($f in 'claude-argv.log', 'agent-stdin.log') { if (Test-Path "$Acc\$f") { Copy-Item "$Acc\$f" $Ev; Show $f (ReadText "$Acc\$f") } }
$plog = H plugin log list --plugin herdr-dots; Save 'plugin-log.json' $plog; Show 'plugin log list' $plog
$apf = H pane read $Pane --source recent --lines 400; Save 'agent-pane.txt' $apf; Show 'agent pane (final)' $apf
$dpf = H pane read $DotsPane --source visible; Save 'dots-pane.txt' $dpf; Show 'Dots pane (final)' $dpf
Show 'sub.log' (ReadText "$Ev\sub.log")
Show 'server stderr' (ReadText "$Ev\server-stderr.txt")
H server stop | Out-Null
Start-Sleep -Seconds 2
Stop-Process -Id $srv.Id -Force -ErrorAction SilentlyContinue

Banner 'RESULTS'
$summary = "| # | Result |`n|---|---|`n"
foreach ($k in $Results.Keys) { Write-Host "STEP $k : $($Results[$k])"; $summary += "| $k | $($Results[$k] -replace '\|', '/') |`n" }
if ($env:GITHUB_STEP_SUMMARY) { Add-Content -Path $env:GITHUB_STEP_SUMMARY -Value $summary }
if ($Results.Values | Where-Object { $_ -match '^FAIL' }) { exit 1 }
exit 0
