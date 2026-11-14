# Guarda de las reglas fijas de CLAUDE.md que se pueden comprobar antes de ejecutar una herramienta.
# La llama .claude/settings.json en PreToolUse (Bash, PowerShell, Write, Edit, NotebookEdit). Lee
# por la entrada estándar el JSON de la llamada y, si una regla aplica, responde con
# permissionDecision «deny» (prohibido), «ask» (Javier decide) o sólo additionalContext (un
# recordatorio para el modelo). Si nada aplica, no escribe nada y la llamada sigue.
#
# No es exhaustiva: las reglas de CLAUDE.md valen también donde no salta. Si falla ella misma, deja
# pasar la llamada (sale con 0 sin escribir nada) para no bloquear el trabajo por un error suyo.
# Cada llamada cuesta el arranque de pwsh (alrededor de medio segundo).
#
# Probarla: pwsh -NoProfile -File .claude/hooks/guard.tests.ps1

$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)

$script:notes = [System.Collections.Generic.List[string]]::new()

function Answer([string]$decision, [string]$reason) {
    @{ hookSpecificOutput = @{ hookEventName = 'PreToolUse'; permissionDecision = $decision; permissionDecisionReason = $reason } } | ConvertTo-Json -Depth 4 -Compress
    exit 0
}
function Note([string]$text) { $script:notes.Add($text) }
function Finish {
    if ($script:notes.Count -gt 0) {
        @{ hookSpecificOutput = @{ hookEventName = 'PreToolUse'; additionalContext = ($script:notes -join "`n") } } | ConvertTo-Json -Depth 4 -Compress
    }
    exit 0
}

# Nombres que no se escriben en este repositorio público (CLAUDE.md, «Nombres»). Se guardan como
# SHA-256 de la palabra en minúsculas para no escribirlos aquí tampoco.
$bannedHashes = @(
    '64622964bfa12a04e4bac3ba500416683abd55d61fccb3041cff26fe5bd0e062'
    'a00f708b02a29dc0ab88da2c290864949fc92f7eb357e8cec9ca5cf5caaa50ad'
) + @(([string]$env:KARPO_GUARD_EXTRA_HASHES) -split '[,\s]+' | Where-Object { $_ })  # para las pruebas
function Test-BannedName([string]$text) {
    if (-not $text) { return $false }
    $words = [System.Collections.Generic.HashSet[string]]::new()
    foreach ($m in [regex]::Matches($text, '\p{L}+')) {
        [void]$words.Add($m.Value.ToLowerInvariant())
        # También cada trozo de una palabra en camelCase o PascalCase.
        foreach ($p in [regex]::Matches($m.Value, '\p{Lu}+(?!\p{Ll})|\p{Lu}?\p{Ll}+')) { [void]$words.Add($p.Value.ToLowerInvariant()) }
    }
    $sha = [System.Security.Cryptography.SHA256]::Create()
    foreach ($w in $words) {
        $h = -join ($sha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($w)) | ForEach-Object { $_.ToString('x2') })
        if ($bannedHashes -contains $h) { return $true }
    }
    return $false
}

# Secretos que nunca deben quedar en un fichero.
$secretPattern = '-----BEGIN [A-Z ]*PRIVATE KEY-----|\bghp_[A-Za-z0-9]{30,}|\bgithub_pat_[A-Za-z0-9_]{30,}|\bsk-ant-[A-Za-z0-9_-]{20,}|\bAKIA[0-9A-Z]{16}\b'

# La reescritura de la historia (CLAUDE.md, «Git»): todo commit que no esté en origin/main y sea
# anterior a esta fecha es de la historia antigua.
$rewriteCutoff = [DateTimeOffset]::new(2026, 10, 17, 0, 0, 0, [TimeSpan]::Zero).ToUnixTimeSeconds()

# Ojo: PowerShell no distingue mayúsculas; una función llamada «Git» se llamaría a sí misma.
function Invoke-Git([string]$dir, [string[]]$gitArgs) {
    $r = & git.exe -C $dir @gitArgs 2>$null
    if ($LASTEXITCODE -ne 0) { return $null }
    return $r
}
function Test-OldHistory([string]$rev) {
    if (-not (Invoke-Git $script:cwd @('rev-parse', '--verify', '--quiet', "$rev^{commit}"))) { return $false }
    if (-not (Invoke-Git $script:cwd @('rev-parse', '--verify', '--quiet', 'origin/main^{commit}'))) { return $false }
    $dates = Invoke-Git $script:cwd @('log', '--format=%ct', $rev, '--not', 'origin/main')
    foreach ($d in @($dates)) { if ($d -and [long]$d -lt $rewriteCutoff) { return $true } }
    return $false
}
# La raíz del repositorio (o worktree) que contiene una ruta, aunque el fichero aún no exista.
function Get-RepoRoot([string]$path) {
    $dir = Split-Path -Parent $path
    while ($dir -and -not (Test-Path -LiteralPath $dir)) { $dir = Split-Path -Parent $dir }
    if (-not $dir) { return $null }
    $top = Invoke-Git $dir @('rev-parse', '--show-toplevel')
    if (-not $top) { return $null }
    return ([string]$top -replace '\\', '/').TrimEnd('/')
}

# Otro repositorio de C:\Git que no es éste (ni su clon limpio): sólo se lee.
$siblingRoot = '(?:C:|/c)[\\/]+Git[\\/]+(?!Paranoia[\\/]+(?:Karpo\.Fw\.Go|karpo-fw-go)(?:[\\/"''\s]|$))'
$siblingPath = '(?i)' + $siblingRoot + '[^\\/\s"'';|]+'

try {
    $call = [Console]::In.ReadToEnd() | ConvertFrom-Json
    $tool = [string]$call.tool_name
    $in = $call.tool_input
    $script:cwd = if ($call.cwd) { [string]$call.cwd } elseif ($env:CLAUDE_PROJECT_DIR) { $env:CLAUDE_PROJECT_DIR } else { (Get-Location).Path }

    if ($tool -in 'Bash', 'PowerShell') {
        $c = [string]$in.command
        # «git -C <dir> push» y «git -c k=v commit» cuentan como «git push» y «git commit».
        $gitOpts = '(?i)\bgit(\.exe)?((\s+-C\s+("[^"]*"|''[^'']*''|\S+))|(\s+-c\s+\S+))+'
        $norm = $c -replace $gitOpts, 'git'
        # Los textos de commit, PR y here-strings pueden citar los patrones sin usarlos: se vacían.
        $masked = $c -replace '(?s)@''.*?''@|@".*?"@', '""'
        $masked = $masked -replace '(?s)<<-?\s*[''"]?(\w+)[''"]?.*?\n\1\b', ''
        $masked = $masked -replace '(?s)(-m|--message|--body|--title|--subject|-b|-t)(\s+|=)("[^"]*"|''[^'']*'')', '$1 ""'
        # Cada orden de la cadena por separado; las que sólo leen o buscan no cuentan. $live guarda
        # las órdenes tal cual (con su «git -C <dir>»); $scan, las mismas con «git» normalizado.
        $readOnly = '^\s*(\(?\s*)(grep|rg|cat|head|tail|less|wc|ls|Select-String|Get-Content|Get-ChildItem|git\s+(grep|log|show|diff|blame|status|ls-files|rev-parse|cat-file|merge-base|branch\s+-[ra]|for-each-ref|fetch))\b'
        $parts = @($masked -split '(?:&&|\|\||;|\||\r?\n)' | Where-Object { $_.Trim() })
        $live = @($parts | Where-Object { ($_ -replace $gitOpts, 'git') -notmatch $readOnly })
        $scan = ($live | ForEach-Object { $_ -replace $gitOpts, 'git' }) -join ' ; '

        # --- Git ---
        if ($scan -match '(?i)\bgit\b[^;]*(--no-verify|core\.hooksPath|commit\.gpgsign\s*=\s*false|--no-gpg-sign)') {
            Answer 'deny' 'CLAUDE.md, Git: nunca se saltan los hooks ni la firma.'
        }
        if ($scan -match '(?i)\bgit\s+(remote\s+set-url\b[^;]*--push|config\b[^;]*remote\.[^\s.]+\.pushurl)') {
            Answer 'deny' 'CLAUDE.md, Git: el pushurl de origin es inválido a propósito en este checkout de historia antigua; no se arregla. Para empujar una rama sacada de origin/main: git push https://github.com/jhermoso/karpo-fw-go.git <rama>.'
        }
        foreach ($push in [regex]::Matches($scan, '(?i)\bgit\s+push\b([^;]*)')) {
            $args_ = $push.Groups[1].Value
            if ($args_ -match '(?i)\s--(all|mirror)\b') {
                Answer 'deny' 'CLAUDE.md, Git: «git push --all/--mirror» subiría las ramas de la historia antigua. Se empuja sólo la rama de la tarea.'
            }
            if ($args_ -match '(?i)[\s:+](refs/heads/)?(main|master)(\s|$)') {
                Answer 'deny' 'CLAUDE.md, Git: a main no se empuja; se empuja la rama de la tarea y se abre una PR.'
            }
            if ($args_ -match '(?i)(\s--force\b|\s-f\b|\s--force-with-lease\b|\s\+[\w/.-]+)') {
                Answer 'ask' 'CLAUDE.md, Git: empujar a la fuerza reescribe historia publicada; sólo si Javier lo pide.'
            }
            if ($args_ -match '(?i)(\s--delete\b|\s-d\b|\s:[\w/.-]+)') {
                Answer 'ask' 'CLAUDE.md, Git: borrar una rama remota sólo si Javier lo pide.'
            }
            # Lo que se empuja no puede traer la historia antigua.
            $tokens = @($args_ -split '\s+' | Where-Object { $_ -and $_ -notmatch '^-' })
            $refs = @($tokens | Select-Object -Skip 1 | ForEach-Object { ($_ -split ':')[0].TrimStart('+') } | Where-Object { $_ })
            if ($refs.Count -eq 0) { $refs = @('HEAD') }
            foreach ($r in $refs) {
                if (Test-OldHistory $r) {
                    Answer 'deny' ("CLAUDE.md, Git: «{0}» lleva commits de la historia antigua (anteriores al 2026-10-17 y fuera de origin/main). Saca la rama de origin/main y lleva allí sólo tus commits (git cherry-pick). Si de verdad son de la historia nueva, que lo decida Javier." -f $r)
                }
            }
        }
        # La main local de este checkout es de la historia antigua: no sirve de base.
        if ($scan -match '(?i)\bgit\s+(merge|rebase|switch|checkout|branch|worktree\s+add|reset|cherry-pick)\b[^;]*(?<![\w/.-])main(?![\w/.-])' -and (Test-OldHistory 'refs/heads/main')) {
            Answer 'deny' 'CLAUDE.md, Git: la main local de este checkout es de la historia antigua. Usa origin/main (git fetch origin; git switch -c <rama> origin/main).'
        }
        if ($scan -match '(?i)\bgh\s+pr\s+merge\b') {
            if ($scan -match '(?i)(\s-d\b|--delete-branch)') {
                Answer 'ask' 'CLAUDE.md, Git: las ramas no se borran sin preguntar a Javier; fusiona sin --delete-branch.'
            }
            Note 'CLAUDE.md, Git: sólo fusiona la sesión de orquestación, y sólo sus PR (D-139), con la batería en verde y la revisión independiente sin bloqueos; un carril no fusiona. Se le cuenta a Javier.'
        }
        # Nombres vetados en lo que se publica: el texto de la orden y los ficheros de mensaje.
        if ($norm -match '(?i)\b(git\s+(commit|tag|notes)|gh\s+(pr|issue|release)\s+(create|edit|comment|review|merge))\b') {
            $published = $c
            foreach ($f in [regex]::Matches($norm, '(?i)(?:\s-F|--file|--body-file)(?:\s+|=)("[^"]+"|''[^'']+''|\S+)')) {
                $fp = $f.Groups[1].Value.Trim('"', "'")
                if (-not [IO.Path]::IsPathRooted($fp)) { $fp = Join-Path $script:cwd $fp }
                if (Test-Path -LiteralPath $fp -PathType Leaf) { $published += "`n" + (Get-Content -Raw -LiteralPath $fp) }
            }
            if (Test-BannedName $published) {
                Answer 'deny' 'CLAUDE.md, Nombres: ese texto lleva un nombre que no se escribe en este repositorio público (el ERP original es «Trench»; su vertical financiera, «FinancialDetail»). Si es la palabra común, usa otra.'
            }
        }

        # --- Otros repositorios: sólo se leen ---
        foreach ($part in $live) {
            if ($part -notmatch $siblingPath) { continue }
            $writes = $part -match '(?i)\bgit\b[^;]*\s(commit|push|checkout|switch|reset|merge|rebase|stash|add|rm|clean|restore|pull|am|apply)(\s|$)' -or
                $part -match '(?i)^\s*(Set-Content|Add-Content|Out-File|Remove-Item|Move-Item|New-Item|Rename-Item|rm|mv|touch|mkdir|sed\s+-i)\b' -or
                $part -match ('(?i)>\s*["'']?' + $siblingRoot) -or
                $part -match ('(?i)^\s*(cp|Copy-Item)\b.*\s["'']?' + $siblingRoot + '[^\s]*["'']?\s*$')
            if ($writes) {
                Answer 'deny' 'CLAUDE.md, Proyectos hermanos: los otros repositorios sólo se leen. Si hace falta un cambio allí, se anota y se pide a su sesión de orquestación.'
            }
        }

        # --- Dependencias ---
        if ($scan -match '(?i)\bgo\s+get\b|\bgo\s+mod\s+edit\b[^;]*-(require|replace)') {
            Answer 'ask' 'CLAUDE.md, Código: ninguna dependencia nueva en un go.mod sin el visto bueno de Javier.'
        }
        if ($scan -match '(?i)\bgo\s+mod\s+tidy\b') {
            Note 'CLAUDE.md, Código: si «go mod tidy» añade un módulo nuevo a go.mod, eso es una dependencia nueva y necesita el visto bueno de Javier (git diff go.mod).'
        }

        # --- Bases de datos ---
        if ($scan -match '(?i)(:55432\b|Port\s*=\s*55432\b|\.supabase\.(co|com)\b)') {
            Answer 'deny' 'CLAUDE.md, Datos: eso apunta a una base viva (la de desarrollo de Karpo C#, puerto 55432, o un proyecto real de Supabase). Sólo bases desechables: SQLite temporal o los contenedores de integration/ en un slot propio.'
        }
        foreach ($m in [regex]::Matches($scan, '(?i)\b(?:postgres(?:ql)?|sqlserver|oracle)://[^@\s"'']+@([^:/?\s"'']+)|@tcp\(([^:)\s]+)')) {
            $h = ($m.Groups[1].Value + $m.Groups[2].Value).ToLowerInvariant()
            if ($h -notin 'localhost', '127.0.0.1', '[::1]', '::1', 'host.docker.internal') {
                Answer 'ask' ("CLAUDE.md, Datos: esa cadena de conexión apunta a «{0}», que no es un contenedor local. Sólo bases desechables." -f $h)
            }
        }
        if ($scan -match '(?i)\bdrop\s+database\b|\bdropdb\b') {
            Answer 'ask' 'CLAUDE.md, Datos: borrar una base sólo si es desechable y la creó esta sesión.'
        }

        # --- La suite de integración (integration/CLAUDE.md) ---
        foreach ($part in $live) {
            if ($part -notmatch '(?i)^\s*(&\s*)?((pwsh|powershell)(\.exe)?\s+(-\w+\s+)*)?["'']?(\.{1,2}[\\/])?(integration[\\/])?run\.ps1\b') { continue }
            $slot = [regex]::Match($part, '(?i)-Slot[:\s]+(\d+)')
            if (-not $slot.Success -or [int]$slot.Groups[1].Value -eq 0) {
                Answer 'deny' 'integration/CLAUDE.md: la suite va en un slot propio (./integration/run.ps1 -Slot N -Down, N de 1 a 99). El slot 0 es el que usa un run.ps1 sin -Slot: dos ejecuciones sobre los mismos servidores se rompen entre sí.'
            }
            $n = [int]$slot.Groups[1].Value
            if ($n -gt 99) {
                Answer 'deny' 'integration/CLAUDE.md: el slot va de 1 a 99; con 100 o más los puertos de Supabase pisan los de PostgreSQL de otros slots.'
            }
            $busy = $null
            try { $busy = & docker ps -q --filter "label=com.docker.compose.project=integration-s$n" 2>$null } catch { }
            if ($busy) {
                Answer 'deny' ("integration/CLAUDE.md: el slot {0} ya tiene contenedores en marcha (integration-s{0}); quizá es la ejecución de otra sesión. Elige otro slot; si los dejó una ejecución tuya interrumpida, quítalos con docker compose -p integration-s{0} down -v." -f $n)
            }
            Note 'integration/CLAUDE.md: antes, comprueba que los cinco puertos del slot (PostgreSQL 55433, Supabase 54433, SQL Server 51433, Oracle 51521, MySQL 53306, +10·N) están fuera de los rangos reservados (netsh interface ipv4 show excludedportrange protocol=tcp). La suite tarda 40-60 minutos: en segundo plano.'
        }
        $project = '(?i)(\s-p|--project-name)(\s+|=)integration(\s|$)'
        $anyProject = '(?i)(\s-p|--project-name)(\s+|=)'
        if ($scan -match '(?i)\bdocker\s+compose\b[^;]*\bup\b' -and ($scan -match $project -or $scan -notmatch $anyProject)) {
            Answer 'deny' 'integration/CLAUDE.md: ése es el proyecto de Compose del slot 0. Arranca la suite con ./integration/run.ps1 -Slot N (N de 1 a 99).'
        }
        if ($scan -match '(?i)\bdocker\b[^;]*\s(down|rm|stop|kill|prune)\b|\bdocker\s+volume\s+rm\b') {
            if ($scan -match $project -or ($scan -match '(?i)\bdocker\s+compose\b[^;]*\bdown\b' -and $scan -notmatch $anyProject)) {
                Answer 'deny' 'integration/CLAUDE.md: ése es el proyecto de Compose del slot 0, que usan las demás sesiones. Sólo se paran o borran los contenedores del proyecto (integration-sN) que arrancó esta sesión.'
            }
            Note 'integration/CLAUDE.md: sólo se paran o borran los contenedores del proyecto que arrancó esta sesión.'
        }
        if ($scan -match '(?i)\bnet\s+stop\s+winnat\b|\bnetsh\b[^;]*\b(add|delete)\s+excludedportrange\b') {
            Answer 'deny' 'integration/CLAUDE.md: liberar los puertos reservados es un cambio del sistema; se le deja a Javier. Elige un slot cuyos puertos estén libres.'
        }

        # --- Formato ---
        if ($c -match '(?i)\bgofmt\b[^;&|]*\s-w\b') {
            $targets = @(([regex]::Match($c, '(?i)\bgofmt\b([^;&|]*)').Groups[1].Value -split '\s+') | Where-Object { $_ -and $_ -notmatch '^-' })
            if ($targets.Count -eq 0 -or @($targets | Where-Object { $_ -notmatch '\.go["'']?$' }).Count -gt 0) {
                Answer 'deny' 'CLAUDE.md, Comprobar: «gofmt -w» sobre una carpeta (o sobre lo que le llega por una tubería) pasa a LF todos los ficheros CRLF del checkout. Úsalo sólo con los .go que has cambiado, uno a uno.'
            }
        }
        Finish
    }

    if ($tool -in 'Write', 'Edit', 'NotebookEdit') {
        $p = [string]($in.file_path ?? $in.notebook_path)
        $abs = $p -replace '\\', '/'
        $text = [string]($in.content ?? $in.new_string ?? $in.new_source)

        if ($p -match $siblingPath) {
            Answer 'deny' 'CLAUDE.md, Proyectos hermanos: los otros repositorios sólo se leen. Si hace falta un cambio allí, se anota y se pide a su sesión de orquestación.'
        }
        # El repositorio del fichero: el de la sesión o uno de sus worktrees.
        $root = Get-RepoRoot $abs
        $origin = if ($root) { [string](Invoke-Git $root @('remote', 'get-url', 'origin')) } else { '' }
        $inside = $root -and $origin -match 'jhermoso/karpo-fw-go(\.git)?$'
        if (-not $inside) {
            # Fuera del repositorio sólo lo de Claude: temporales, memoria, planes y notas entre sesiones.
            $allowed = @($env:TEMP, $env:TMP, (Join-Path $HOME '.claude'), (Join-Path $env:LOCALAPPDATA 'Claude-Data')) | Where-Object { $_ } | ForEach-Object { ($_ -replace '\\', '/').TrimEnd('/') + '/' }
            if (-not ($allowed | Where-Object { $abs.StartsWith($_, [StringComparison]::OrdinalIgnoreCase) })) {
                Answer 'ask' ("CLAUDE.md: «{0}» está fuera de este repositorio." -f $p)
            }
            Finish
        }
        $rel = $abs.Substring($root.Length).TrimStart('/')

        if ($text -match $secretPattern) {
            Answer 'deny' 'CLAUDE.md, Código: eso parece un secreto (clave privada o token). Ninguno en código ni en configuración.'
        }
        if (Test-BannedName $text) {
            Answer 'deny' 'CLAUDE.md, Nombres: ese texto lleva un nombre que no se escribe en este repositorio público (el ERP original es «Trench»; su vertical financiera, «FinancialDetail»). Si es la palabra común, usa otra.'
        }
        if ($rel -match '(^|/)go\.mod$') {
            Answer 'ask' 'CLAUDE.md, Código: ninguna dependencia nueva en un go.mod sin el visto bueno de Javier.'
        }
        # El dominio es puro: pkg/domain y pkg/domain/spec sólo biblioteca estándar y el propio
        # dominio; el resto de pkg/domain admite shopspring/decimal; el dominio de un contexto, la
        # biblioteca estándar, pkg/domain y su propio dominio.
        if ($rel -match '^(pkg/domain|contexts/[^/]+/domain)(/.*)?/[^/]+\.go$' -and $rel -notmatch '_test\.go$') {
            $ok = @('github.com/jhermoso/karpo-fw-go/pkg/domain')
            if ($rel -match '^contexts/([^/]+)/') { $ok += "github.com/jhermoso/karpo-fw-go/contexts/$($Matches[1])/domain" }
            $decimalOk = $rel -match '^pkg/domain/' -and $rel -notmatch '^pkg/domain/(spec/)?[^/]+$'
            $foreign = @([regex]::Matches($text, '(?m)^\s*(?:import\s+)?(?:[\w.]+\s+)?"([a-z0-9-]+\.[a-z.]+/[^"]+)"') | ForEach-Object { $_.Groups[1].Value } |
                Where-Object { $imp = $_; -not ($ok | Where-Object { $imp -eq $_ -or $imp.StartsWith($_ + '/') }) -and -not ($decimalOk -and $imp -eq 'github.com/shopspring/decimal') })
            if ($foreign.Count -gt 0) {
                Answer 'ask' ("CLAUDE.md, Código: el dominio es puro; esto importa {0}." -f ($foreign -join ', '))
            }
        }
        if ($rel -eq 'docs/DECISIONES-COMUNES.md') {
            $rowId = '(?m)^\|\s*(D-\d+)\s*\|'
            $before = @(); $after = $text
            if ($tool -eq 'Edit') { $before = @([regex]::Matches([string]$in.old_string, $rowId) | ForEach-Object { $_.Groups[1].Value }) }
            elseif (Test-Path -LiteralPath $p) { $before = @([regex]::Matches((Get-Content -Raw -LiteralPath $p), $rowId) | ForEach-Object { $_.Groups[1].Value }) }
            $have = @([regex]::Matches($after, $rowId) | ForEach-Object { $_.Groups[1].Value })
            $gone = @($before | Where-Object { $have -notcontains $_ } | Select-Object -Unique)
            if ($gone.Count -gt 0) {
                Answer 'deny' ("docs/CLAUDE.md: una fila del registro no se borra nunca ({0}); si una decisión cambia, se anota la nueva y a la antigua sólo se le añade «sustituida por D-n»." -f ($gone -join ', '))
            }
            Note 'docs/CLAUDE.md: el registro lo mantiene «Orquestador Karpo Go»; un carril sólo arregla su forma y propone las filas nuevas en su PR.'
        }
        if ($rel -match '^contexts/[^/]+/infrastructure/' -and ($text -match 'Migration\{|Version:\s*\d|Migrations\(\)' -or $rel -match '(persistence|schema)\.go$')) {
            Note 'contexts/CLAUDE.md: una migración aplicada no se toca; se añade la siguiente.'
        }
        Finish
    }
} catch { exit 0 }

exit 0
