# Interactive Windows check of Herdr Dots' popups and keybindings: the
# Linux interactive method (docs/superpowers/notes/2026-10-11-linux-interactive.md)
# with conptyh (scripts/acceptance/windows/conpty) in place of tmux.
#
# The real Herdr client (`herdr --session acc`) runs in a 200x50 ConPTY;
# conptyh keeps the screen in a VT emulator, presses keys one write at a
# time, waits for text and resizes. The agent and the dot's CLI are the
# stand-ins of the headless run (run.ps1): claude.exe (fakeclaude, agent
# mode) in the pane, claude.cmd (fakeclaude, review mode) for the dot.
#
# Usage: run-interactive.ps1 -Herdr <herdr.exe> -Tools <dir with conptyh.exe, acctool.exe, fakeclaude.exe> -Repo <checkout>
param(
    [Parameter(Mandatory)] [string]$Herdr,
    [Parameter(Mandatory)] [string]$Tools,
    [Parameter(Mandatory)] [string]$Repo
)
$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$OutputEncoding = [Text.Encoding]::UTF8

$Acc = 'C:\acc'
$Ev = Join-Path $Acc 'evidence'
$Scr = Join-Path $Ev 'screens'
$App = 'C:\work\app'
$Sid = '0a1b2c3d-1111-4222-8333-444455556666'
$Utf8 = [System.Text.UTF8Encoding]::new($false)
$env:ACC_DIR = $Acc
$AccTool = Join-Path $Tools 'acctool.exe'
$Conptyh = Join-Path $Tools 'conptyh.exe'
$Addr = '127.0.0.1:47011'

$Steps = [ordered]@{
    '1' = 'prefix+d opens manage; first-run Setup pager; n; the library'
    '2' = 't, Security template, ctrl+s, enter attaches it to the agent pane'
    '3' = 'prefix+a shows the Dots pane and roster'
    '4' = 'a turn ends and a card appears; tab, then m cycles the mode'
    '5' = 'prefix+shift+s summon: a question, the answer arrives as a card'
    '6' = 'prefix+shift+c council: a card'
    '7' = 'M memory screen; E export defaults to the home dir; written to a temp path'
    '8' = '60x15 with a popup open: Too small, keys refused'
    'B1' = 'a fast key burst (jjj in one write) moves the template cursor 3 rows'
}
$Results = [ordered]@{}
foreach ($k in $Steps.Keys) { $Results[$k] = 'NOT RUN' }
function Result([string]$n, [string]$status, [string]$why) {
    $Results[$n] = "$status - $why"
    Write-Host ""
    Write-Host "########## STEP $n : $status - $why"
    Write-Host ""
}
function Banner([string]$t) { Write-Host ""; Write-Host "==================== $t ====================" }
function Hd { $out = & $Herdr --session acc @args 2>&1 | Out-String; return $out }
function Show([string]$label, [string]$text) { Write-Host "--- $label"; Write-Host $text }
function ReadText([string]$p) {
    if (-not $p -or -not (Test-Path -LiteralPath $p)) { return '' }
    try {
        $fs = [IO.FileStream]::new($p, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete)
        try { return [IO.StreamReader]::new($fs, $Utf8).ReadToEnd() } finally { $fs.Dispose() }
    } catch { return "(unreadable: $($_.Exception.Message))" }
}
function Save([string]$name, [string]$text) { [IO.File]::WriteAllText((Join-Path $Ev $name), $text, $Utf8) }
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

# ---- the conptyh client ----
$script:HCode = 0
function H([string]$line) {
    $out = & $Conptyh cmd --addr $Addr $line 2>&1 | Out-String
    $script:HCode = $LASTEXITCODE
    return $out
}
function Keys([string]$keys) { $null = H "keys $keys" }
$script:ShotSeq = 0
# Dump: print and save the current screen under a label.
function Dump([string]$label) {
    $s = H 'screen'
    $script:ShotSeq++
    $name = '{0:D2}-{1}.txt' -f $script:ShotSeq, $label
    [IO.File]::WriteAllText((Join-Path $Scr $name), $s, $Utf8)
    Write-Host "----- screen [$label] (200 cols unless resized) -----"
    Write-Host $s
    Write-Host "----- end screen [$label] -----"
    return $s
}
# WaitS: wait for a regex on the screen, then dump it. Returns the screen when
# it matched, else $null (the dump shows what was there).
function WaitS([double]$secs, [string]$regex, [string]$label) {
    $s = H "wait-for $secs $regex"
    $ok = ($script:HCode -eq 0)
    $script:ShotSeq++
    $name = '{0:D2}-{1}.txt' -f $script:ShotSeq, $label
    [IO.File]::WriteAllText((Join-Path $Scr $name), $s, $Utf8)
    Write-Host "----- screen [$label] wait-for /$regex/ : $(if ($ok) { 'matched' } else { 'NOT matched' }) -----"
    Write-Host $s
    Write-Host "----- end screen [$label] -----"
    if ($ok) { return $s }
    return $null
}
function WaitQuiet([double]$secs, [string]$regex) {
    $s = H "wait-for $secs $regex"
    if ($script:HCode -eq 0) { return $s }
    return $null
}
function WaitGone([double]$secs, [string]$regex) {
    $s = H "wait-gone $secs $regex"
    return ($script:HCode -eq 0)
}

# ---- transcript and agent screens (as in run.ps1) ----
$TDir = Join-Path $env:USERPROFILE '.claude\projects\C--work-app'
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
function AgentScreen([string]$cmd) {
    $script:ScreenSeq++
    [IO.File]::WriteAllText((Join-Path $Acc ('screen\{0:D4}.txt' -f $script:ScreenSeq)), $cmd, $Utf8)
}
function Feed { ReadText (Join-Path $script:SessDir 'feed.jsonl') }
function FeedTurn([int]$n) {
    foreach ($l in ((Feed) -split "`n")) { if ($l -match "`"turn`":$n,") { return $l } }
    return $null
}
function Cycle([int]$n, [string]$user, [string]$assistant) {
    AgentScreen 'working'
    Start-Sleep -Seconds 3
    Turn $n $user $assistant
    AgentScreen "idle $n"
    $line = WaitFor { FeedTurn $n } 90 "feed entry for turn $n"
    Show "feed turn $n" "$line"
    return $line
}
function PaneIds { $o = Hd pane list; return @([regex]::Matches($o, '"pane_id"\s*:\s*"([^"]+)"') | ForEach-Object { $_.Groups[1].Value } | Select-Object -Unique) }
function FindSessDir {
    $engLog = Get-ChildItem -LiteralPath $env:APPDATA, $env:LOCALAPPDATA -Recurse -Force -File -Filter engine.log -ErrorAction SilentlyContinue |
        Where-Object { $_.FullName -match 'herdr-dots' } | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if ($engLog) { return $engLog.DirectoryName }
    return ''
}
function MemoryFiles {
    Get-ChildItem -LiteralPath $script:StateDir, $script:CfgDir -Recurse -Force -File -Filter '*.json' -ErrorAction SilentlyContinue |
        Where-Object { (ReadText $_.FullName) -match '"entries"' }
}
# Popup opens by its keybinding; when the binding does nothing, the action is
# invoked from the CLI so the later steps can still run (the step fails).
function OpenPopup([string]$keys, [string]$action, [string]$regex, [string]$label) {
    Keys $keys
    $s = WaitS 20 $regex $label
    if ($s) { return @{ ok = $true; screen = $s } }
    Write-Host "keybinding '$keys' did not open '$action'; invoking it from the CLI to go on"
    Show "plugin action invoke $action" (Hd plugin action invoke $action --plugin herdr-dots)
    $s = WaitS 20 $regex "$label-cli"
    return @{ ok = $false; screen = $s }
}

# ---- setup ----
Banner 'setup'
foreach ($d in @($Acc, $Ev, $Scr, "$Acc\bin", "$Acc\agentbin", "$Acc\fake", "$Acc\screen", $App)) { New-Item -ItemType Directory -Force $d | Out-Null }
Copy-Item (Join-Path $Tools 'fakeclaude.exe') "$Acc\fake\fakeclaude.exe"
Copy-Item (Join-Path $Tools 'fakeclaude.exe') "$Acc\agentbin\claude.exe"
[IO.File]::WriteAllText("$Acc\bin\claude.cmd", "@echo off`r`n`"%~dp0..\fake\fakeclaude.exe`" %*`r`n", [Text.Encoding]::ASCII)
$herdrDir = Split-Path $Herdr
$env:PATH = "$Acc\bin;$herdrDir;$env:PATH"
Write-Host "claude resolves to: $((Get-Command claude -ErrorAction SilentlyContinue).Source)"
[IO.File]::WriteAllText("$App\server.js", "const express = require('express');`nconst app = express();`nconst PASSWORD = `"hunter2`";`napp.post('/login', (req, res) => { db.query(`"SELECT * FROM users WHERE name = '`" + req.body.user + `"'`"); });`n", $Utf8)
git -C $App init -q 2>&1 | Out-Null
git -C $App -c user.name=acc -c user.email=acc@example.invalid add -A 2>&1 | Out-Null
git -C $App -c user.name=acc -c user.email=acc@example.invalid commit -q -m init 2>&1 | Out-Null

Show 'herdr --version' (& $Herdr --version 2>&1 | Out-String)
$defcfg = & $Herdr --default-config 2>&1 | Out-String
Show 'herdr --default-config (prefix lines)' (($defcfg -split "`n" | Where-Object { $_ -match 'prefix\s*=|^\[keys\]' }) -join "`n")
$inst = & $Herdr plugin install GustavoGomez092/herdr-dots --yes 2>&1 | Out-String
Show "herdr plugin install (exit $LASTEXITCODE)" $inst
$manifest = Get-ChildItem (Join-Path $env:APPDATA 'herdr\plugins') -Recurse -Force -Filter herdr-plugin.toml -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -match 'herdr-dots' } | Select-Object -First 1
$PluginDir = if ($manifest) { $manifest.DirectoryName } else { '' }
$Dots = Join-Path $PluginDir 'bin\herdr-dots.exe'
$ver = & $Dots --version 2>&1 | Out-String
Show "plugin dir $PluginDir; --version" $ver
Save 'version.txt' $ver
Push-Location $PluginDir
$sk = & $Dots setup-keys --yes 2>&1 | Out-String
Pop-Location
Show 'herdr-dots setup-keys --yes' $sk
$HerdrCfg = Join-Path $env:APPDATA 'herdr\config.toml'
Show "Herdr config ($HerdrCfg)" (ReadText $HerdrCfg)
Save 'herdr-config.toml' (ReadText $HerdrCfg)

# ---- the Herdr client in a ConPTY ----
Banner 'herdr client in a 200x50 ConPTY'
$harness = Start-Process -FilePath $Conptyh -PassThru -WindowStyle Hidden -WorkingDirectory $App `
    -ArgumentList 'serve', '--addr', $Addr, '--cols', '200', '--rows', '50', '--dir', $App, '--log', "$Ev\conptyh.log", '--raw', "$Ev\conpty-raw.bin", '--', $Herdr, '--session', 'acc' `
    -RedirectStandardOutput "$Ev\conptyh-stdout.txt" -RedirectStandardError "$Ev\conptyh-stderr.txt"
Write-Host "conptyh pid $($harness.Id)"
Start-Sleep -Seconds 3
$null = WaitS 30 'herdr|spaces|PS C:' 'client-start'
# First-run screens of the Herdr client: the welcome (enter) and the agent
# integrations offer (esc).
for ($i = 0; $i -lt 6; $i++) {
    $s = H 'screen'
    if ($s -match 'continue' -and $s -match 'herdr') { Keys 'enter'; Start-Sleep -Seconds 1.5; continue }
    if ($s -match 'agent integrations') { Keys 'esc'; Start-Sleep -Seconds 1.5; continue }
    if ($s -match 'PS C:\\') { break }
    Start-Sleep -Seconds 2
}
$null = Dump 'client-ready'
Show 'alive' (H 'alive')
Show 'pane list' (Hd pane list)

$Pane = WaitFor { (PaneIds | Select-Object -First 1) } 30 'a pane'
Write-Host "agent pane: $Pane"
$script:SessDir = WaitFor { FindSessDir } 60 'engine.log'
$script:StateDir = if ($script:SessDir) { Split-Path $script:SessDir } else { '' }
$script:Pipe = ''
if ($script:SessDir) { $el = ReadText (Join-Path $script:SessDir 'engine.log'); if ($el -match 'endpoint (\S+)') { $script:Pipe = $Matches[1] } }
Write-Host "engine session dir: $($script:SessDir); pipe: $($script:Pipe)"
$script:CfgDir = (Hd plugin config-dir herdr-dots).Trim()
if ($script:CfgDir -match '"([A-Za-z]:\\\\[^"]+)"') { $script:CfgDir = $Matches[1] -replace '\\\\', '\' }
Write-Host "plugin config dir: $($script:CfgDir)"

# The agent: claude.exe typed into the pane through the client.
Turn 1 'Add a login route to server.js with a password check.' 'I added POST /login to server.js. It compares the password with the PASSWORD constant set on line 3.'
$null = H "type $Acc\agentbin\claude.exe"
Keys 'enter'
$agentUp = WaitFor { $g = Hd pane get $Pane; if ($g -match '"agent"\s*:\s*"claude"') { $g } } 45 'pane agent = claude'
Show 'pane get (agent detected)' "$agentUp"
$o = & $Herdr --session acc pane report-agent-session $Pane --source herdr:claude --agent claude --seq "$([DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds())" --agent-session-id $Sid --agent-session-path $TFile --session-start-source startup 2>&1 | Out-String
Write-Host "report-agent-session exit $LASTEXITCODE $o"
$null = WaitFor { $g = Hd pane get $Pane; if ($g -match [regex]::Escape($Sid)) { $g } } 10 'agent_session on the pane'
Show 'pane get (after the session report)' (Hd pane get $Pane)
$null = Dump 'agent-running'

try {
# ---- step 1: prefix+d -> manage, Setup pager, n, library ----
Banner 'step 1: prefix+d manage, Setup, library'
$p = OpenPopup 'ctrl+b d' 'manage' 'Let your agent consult|DOTS LIBRARY|SETUP|Setup' '01a-prefix-d'
$setupSeen = $p.screen -and $p.screen -match 'Let your agent consult'
Keys 'n'
$lib = WaitS 15 'DOTS LIBRARY' '01b-library'
if ($p.ok -and $setupSeen -and $lib) { Result 1 'PASS' 'prefix+d opened Dots: manage on the first-run Setup pager (MCP offer); n snoozed it; the library shows' }
elseif ($p.ok -and $lib) { Result 1 'FAIL' 'prefix+d opened manage, but no first-run Setup pager was shown' }
elseif ($p.ok) { Result 1 'FAIL' 'prefix+d opened manage, but n did not reach the library' }
else { Result 1 'FAIL' 'prefix+d did not open manage (the CLI action was used to go on)' }
if (-not $lib) { throw 'ABORT: no library' }

# ---- step 2 (+ B1): template, burst, save, attach ----
Banner 'step 2: template, ctrl+s, enter'
Keys 't'
$tl = WaitS 10 'NEW DOT FROM TEMPLATE' '02a-templates'
$first = if ($tl -and $tl -match '(?m)▸\s*\S+\s+(\S.*?)\s*│') { $Matches[1] } else { '?' }
$null = H 'burst jjj'
Start-Sleep -Milliseconds 800
$b1 = WaitS 5 '▸\s*\S+\s+Copywriter' 'B1-burst-jjj'
$now = if ($b1) { 'Copywriter' } else { $s = H 'screen'; if ($s -match '(?m)▸\s*\S+\s+(\S.*?)\s*│') { $Matches[1] } else { '?' } }
if ($b1) { Result 'B1' 'PASS' "one write of 'jjj' moved the cursor 3 rows: '$first' -> 'Copywriter'" }
else { Result 'B1' 'FAIL' "one write of 'jjj' left the cursor on '$now' (from '$first'; 3 rows down is Copywriter)" }
for ($i = 0; $i -lt 16; $i++) {
    if (WaitQuiet 0.5 '▸\s*\S+\s+Security') { break }
    Keys 'j'
}
$null = Dump '02b-security-selected'
Keys 'enter'
$ed = WaitS 10 'Name\s+>\s*Security' '02c-editor'
Keys 'ctrl+s'
$saved = WaitS 10 '\[x\]\s*\S+\s+Security' '02d-saved'
Keys 'enter'
$closed = WaitGone 10 'DOTS LIBRARY'
$attJson = WaitFor { $t = ReadText (Join-Path $script:SessDir 'attachments.json'); if ($t -match '"security"') { $t } } 20 'attachments.json with security'
$side = WaitS 10 'claude\s+\S*1' '02e-attached'
Show 'attachments.json' "$attJson"
if ($ed -and $saved -and $closed -and $attJson) { Result 2 'PASS' "Security template opened in the editor, ctrl+s saved it (checked in the library), enter closed manage and attached it to $Pane (attachments.json)" }
else { Result 2 'FAIL' "editor: $([bool]$ed), saved+checked: $([bool]$saved), closed on enter: $closed, attached: $([bool]$attJson)" }
$catch = WaitFor { FeedTurn 1 } 60 'catch-up review (turn 1)'
Show 'catch-up feed' "$catch"

# ---- step 3: prefix+a -> Dots pane ----
Banner 'step 3: prefix+a Dots pane'
$before = PaneIds
$p = OpenPopup 'ctrl+b a' 'toggle-pane' 'DOTS[^\n]*\n[^\n]*Security' '03-roster'
$DotsPane = WaitFor { PaneIds | Where-Object { $before -notcontains $_ } | Select-Object -First 1 } 10 'the Dots pane'
Write-Host "Dots pane: $DotsPane"
$roster = WaitS 30 'CRITICAL\s+Security' '03b-roster-card'
if ($p.ok -and $roster) { Result 3 'PASS' "prefix+a opened the Dots pane ($DotsPane): roster '◆ Security' and the turn 1 card" }
elseif ($p.ok -and $p.screen) { Result 3 'FAIL' 'prefix+a opened the Dots pane with the roster, but no card showed' }
else { Result 3 'FAIL' 'prefix+a did not open the Dots pane' }

# ---- step 4: a turn ends, a card; tab, m cycles the mode ----
Banner 'step 4: turn 2 card; tab; m'
[IO.File]::WriteAllText("$Acc\fake-finding", 'SQL injection in server.js: the login query concatenates req.body.user into the SQL string. Use a parameterized query.', $Utf8)
[IO.File]::WriteAllText("$Acc\fake-severity", 'warning', $Utf8)
$t2 = Cycle 2 'Look the user up in the database on login.' 'The /login route now queries the users table with the name from req.body.user.'
$card = WaitS 60 'WARN\s+Security\s+·\s+turn 2' '04a-card'
Keys 'ctrl+b l'
Start-Sleep -Milliseconds 500
Keys 'tab'
$null = Dump '04b-tab'
$modes = @()
foreach ($want in 'inject', 'observe', 'suggest', 'queue') {
    Keys 'm'
    $m = WaitS 6 "Security\s+$want" "04c-mode-$want"
    $modes += "$want=$(if ($m) { 'yes' } else { 'no' })"
}
Keys 'tab' # back to the queue, where step 7 acts on cards
if ($card -and ($modes -join ',') -eq 'inject=yes,observe=yes,suggest=yes,queue=yes') { Result 4 'PASS' "turn 2 produced a WARN card; tab to the roster, m cycled queue -> inject -> observe -> suggest -> queue" }
else { Result 4 'FAIL' "card: $([bool]$card); modes: $($modes -join ', ')" }

# ---- step 5: prefix+shift+s summon ----
Banner 'step 5: prefix+shift+s summon'
[IO.File]::WriteAllText("$Acc\fake-finding", 'Yes: the login route is exposed to brute force. Add rate limiting (e.g. 5 attempts per minute per IP) and lock the account after repeated failures.', $Utf8)
[IO.File]::WriteAllText("$Acc\fake-severity", 'info', $Utf8)
$p = OpenPopup 'ctrl+b S' 'summon' 'SUMMON A DOT' '05a-summon'
Keys 'enter'
$q = WaitS 6 'optional question' '05b-question'
$null = H 'type Is the login route safe against brute force?'
$null = Dump '05c-typed'
Keys 'enter'
$ans = WaitS 60 'INFO\s+Security' '05d-answer'
$asked = (ReadText "$Acc\claude-last-stdin.txt") -match 'brute force'
if ($p.ok -and $ans -and $ans -match 'brute force') { Result 5 'PASS' "prefix+shift+s opened summon; Security chosen, the question asked$(if ($asked) { ' (it reached the dot)' }); the answer arrived as an INFO card" }
elseif ($ans) { Result 5 'FAIL' 'the answer card arrived, but prefix+shift+s did not open summon (the CLI action was used)' }
else { Result 5 'FAIL' "summon opened: $($p.ok); question field: $([bool]$q); answer card: no" }

# ---- step 6: prefix+shift+c council ----
Banner 'step 6: prefix+shift+c council'
$p = OpenPopup 'ctrl+b C' 'council' 'COUNCIL' '06a-council'
Keys 'enter'
$cc = WaitS 60 'Council\s+·\s+turn' '06b-council-card'
if ($p.ok -and $cc) { Result 6 'PASS' 'prefix+shift+c opened the council (one member, with an estimate); enter convened it; a council card arrived' }
elseif ($cc) { Result 6 'FAIL' 'a council card arrived, but prefix+shift+c did not open the council (the CLI action was used)' }
else { Result 6 'FAIL' "council opened: $($p.ok); council card: no" }

# ---- step 7: send + dismiss, memory, export ----
Banner 'step 7: memory and export'
Keys 'ctrl+b l'
Start-Sleep -Milliseconds 500
Keys 'j'
Keys 'x'
Start-Sleep -Seconds 1.5
Keys 'k'
Keys 'enter'
Start-Sleep -Seconds 2.5
$null = WaitS 20 'mem 2' '07a-after-send-dismiss'
$p = OpenPopup 'ctrl+b d' 'manage' 'DOTS LIBRARY' '07b-manage'
Keys 'M'
$mem = WaitS 10 'MEMORY' '07c-memory'
Keys 'E'
$ex = WaitS 10 'EXPORT DOT MEMORY' '07d-export'
$homeRe = [regex]::Escape($env:USERPROFILE) + '\\herdr-dots-memory-'
$defaultHome = $ex -and ($ex -match "(?m)>\s*$homeRe")
$Export = Join-Path $env:RUNNER_TEMP 'herdr-dots-mem-export.toml'
if (-not $env:RUNNER_TEMP) { $Export = Join-Path ([IO.Path]::GetTempPath()) 'herdr-dots-mem-export.toml' }
Remove-Item -LiteralPath $Export -ErrorAction SilentlyContinue
Keys 'ctrl+u'
$null = H "type $Export"
$null = Dump '07e-export-path'
Keys 'enter'
$done = WaitS 10 'exported \d+ entr' '07f-export-done'
$written = Test-Path -LiteralPath $Export
if ($written) {
    Show "export file $Export" (ReadText $Export)
    Copy-Item -LiteralPath $Export (Join-Path $Ev 'mem-export.toml')
    Show 'export file ACL' (icacls $Export 2>&1 | Out-String)
}
$memOk = $mem -and $mem -match 'finding' -and $mem -match 'dismissed'
if ($memOk -and $defaultHome -and $done -and $written) { Result 7 'PASS' "M shows the sent finding and the dismissed one; E defaults to $($env:USERPROFILE)\herdr-dots-memory-*.toml; the export was written to $Export" }
else { Result 7 'FAIL' "memory (finding+dismissed): $([bool]$memOk); default in home: $([bool]$defaultHome); exported: $([bool]$done); file: $written" }

# ---- step 8: 60x15 with a popup open ----
Banner 'step 8: Too small'
Keys 'E'
$ex2 = WaitS 10 'EXPORT DOT MEMORY' '08a-export-open'
$homeBefore = @(Get-ChildItem -LiteralPath $env:USERPROFILE -Filter 'herdr-dots-memory-*' -Force -ErrorAction SilentlyContinue).Count
Show 'resize' (H 'resize 60 15')
$small = WaitS 10 'Too small' '08b-60x15'
Keys 'enter'
Start-Sleep -Seconds 1.5
$after = Dump '08c-60x15-after-enter'
$homeAfter = @(Get-ChildItem -LiteralPath $env:USERPROFILE -Filter 'herdr-dots-memory-*' -Force -ErrorAction SilentlyContinue).Count
$refused = ($after -match 'Too small') -and ($homeAfter -eq $homeBefore)
Show 'resize' (H 'resize 200 50')
$back = WaitS 10 'EXPORT DOT MEMORY' '08d-restored'
Keys 'esc'
Start-Sleep -Milliseconds 600
Keys 'esc'
Start-Sleep -Milliseconds 800
$null = Dump '08e-closed'
if ($small -and $refused -and $back) { Result 8 'PASS' "at 60x15 the export screen says Too small; enter was refused (no file in the home dir, still Too small); at 200x50 the export screen is back" }
else { Result 8 'FAIL' "Too small: $([bool]$small); enter refused: $refused (home files $homeBefore -> $homeAfter); restored: $([bool]$back)" }

} catch { Write-Host "stopped: $_" }

# ---- evidence ----
Banner 'evidence'
$null = Dump 'final'
if ($script:SessDir) {
    foreach ($f in 'engine.log', 'feed.jsonl', 'queue.json', 'attachments.json', 'ledger.jsonl') {
        $pp = Join-Path $script:SessDir $f
        if (Test-Path -LiteralPath $pp) { Copy-Item -LiteralPath $pp $Ev; Show $f (ReadText $pp) }
    }
}
foreach ($m in (MemoryFiles)) { Copy-Item -LiteralPath $m.FullName (Join-Path $Ev ("memory-" + $m.Name)); Show "memory $($m.FullName)" (ReadText $m.FullName) }
Copy-Item -LiteralPath $TFile $Ev -ErrorAction SilentlyContinue
foreach ($f in 'claude-argv.log', 'agent-stdin.log') { if (Test-Path "$Acc\$f") { Copy-Item "$Acc\$f" $Ev; Show $f (ReadText "$Acc\$f") } }
$plog = Hd plugin log list --plugin herdr-dots; Save 'plugin-log.json' $plog; Show 'plugin log list' $plog
Show 'pane list' (Hd pane list)
$null = H 'quit'
Hd server stop | Out-Null
Start-Sleep -Seconds 2
Stop-Process -Id $harness.Id -Force -ErrorAction SilentlyContinue
Show 'conptyh.log' (ReadText "$Ev\conptyh.log")
Show 'conptyh stderr' (ReadText "$Ev\conptyh-stderr.txt")

Banner 'RESULTS'
$summary = "| # | Step | Result |`n|---|---|---|`n"
foreach ($k in @($Results.Keys)) { if ($Results[$k] -eq 'NOT RUN') { $Results[$k] = 'BLOCKED - an earlier step failed' } }
foreach ($k in $Results.Keys) { Write-Host "STEP $k : $($Results[$k])"; $summary += "| $k | $($Steps[$k]) | $($Results[$k] -replace '\|', '/') |`n" }
if ($env:GITHUB_STEP_SUMMARY) { Add-Content -Path $env:GITHUB_STEP_SUMMARY -Value $summary }
Save 'results.md' $summary
if ($Results.Values | Where-Object { $_ -match '^FAIL' }) { exit 1 }
exit 0
