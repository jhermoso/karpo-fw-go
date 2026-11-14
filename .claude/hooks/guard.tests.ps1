# Casos de guard.ps1: lo que debe pasar y lo que no. Uso, desde la raíz del repositorio:
#   pwsh -NoProfile -File .claude/hooks/guard.tests.ps1
# Sale con 1 si algún caso no da lo esperado. Los casos de la historia antigua sólo corren en un
# checkout que la tenga (el de C:\Git\Paranoia\Karpo.Fw.Go); en otro salen como «omitido».

$ErrorActionPreference = 'Stop'
$root = (git rev-parse --show-toplevel) -replace '\\', '/'
$guard = Join-Path $PSScriptRoot 'guard.ps1'
$env:CLAUDE_PROJECT_DIR = $root
# La regla de nombres se prueba con una palabra cualquiera, para no escribir aquí los de verdad.
$sha = [System.Security.Cryptography.SHA256]::Create()
$env:KARPO_GUARD_EXTRA_HASHES = -join ($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes('zanahoria')) | ForEach-Object { $_.ToString('x2') })

function Test-Ancestor([string]$rev) { git merge-base --is-ancestor $rev origin/main 2>$null; return $LASTEXITCODE -eq 0 }
$oldCommit = 'f605e6e'
# Un mensaje de commit en fichero, para «git commit -F».
$env:KARPO_GUARD_MSG = Join-Path ([IO.Path]::GetTempPath()) 'karpo-guard-msg.txt'
[IO.File]::WriteAllText($env:KARPO_GUARD_MSG, "docs: viene de Zanahoria`n")
$oldMain =((git cat-file -t $oldCommit 2>$null) -eq 'commit') -and -not (Test-Ancestor $oldCommit)
$localMainOld = [bool](git rev-parse --verify --quiet refs/heads/main) -and -not (Test-Ancestor 'refs/heads/main')

function ShC([string]$cmd) { @{ tool = 'Bash'; input = @{ command = $cmd } } }
function PsC([string]$cmd) { @{ tool = 'PowerShell'; input = @{ command = $cmd } } }
function Wr([string]$path, [string]$content) { @{ tool = 'Write'; input = @{ file_path = $path; content = $content } } }
function Ed([string]$path, [string]$old, [string]$new) { @{ tool = 'Edit'; input = @{ file_path = $path; old_string = $old; new_string = $new } } }

$R = $root
$cases = @(
    # --- Git ---
    @{ n = 'commit normal'; c = (ShC 'git commit -m "docs: limpia CLAUDE.md"'); want = 'allow' }
    @{ n = 'commit sin hooks'; c = (ShC 'git commit --no-verify -m x'); want = 'deny' }
    @{ n = 'mensaje que cita --no-verify'; c = (ShC 'git commit -m "el hook prohíbe --no-verify"'); want = 'allow' }
    @{ n = 'grep que busca --no-verify'; c = (ShC 'grep -rn -- "--no-verify" .claude'); want = 'allow' }
    @{ n = 'arreglar el pushurl'; c = (ShC 'git remote set-url --push origin https://github.com/jhermoso/karpo-fw-go.git'); want = 'deny' }
    @{ n = 'push a main'; c = (ShC 'git push https://github.com/jhermoso/karpo-fw-go.git HEAD:main'); want = 'deny' }
    @{ n = 'push de main'; c = (ShC 'git push origin main'); want = 'deny' }
    @{ n = 'push --all'; c = (ShC 'git push --all origin'); want = 'deny' }
    @{ n = 'push forzado'; c = (ShC 'git push --force origin carril/x'); want = 'ask' }
    @{ n = 'push de rama con main en el nombre'; c = (ShC 'git push https://github.com/jhermoso/karpo-fw-go.git HEAD:refs/heads/fix/main-menu'); want = 'allow' }
    @{ n = 'push de la rama actual'; c = (ShC 'git push https://github.com/jhermoso/karpo-fw-go.git HEAD'); want = 'allow' }
    @{ n = 'push de historia antigua'; c = (ShC "git push https://github.com/jhermoso/karpo-fw-go.git ${oldCommit}:refs/heads/x"); want = 'deny'; only = $oldMain }
    @{ n = 'rama desde la main local antigua'; c = (ShC 'git switch -c carril/x main'); want = 'deny'; only = $localMainOld }
    @{ n = 'rama desde origin/main'; c = (ShC 'git switch -c carril/x origin/main'); want = 'allow' }
    @{ n = 'merge-base con main'; c = (ShC 'git merge-base --is-ancestor main origin/main'); want = 'allow' }
    @{ n = 'fusionar una PR'; c = (ShC 'gh pr merge 90 --merge'); want = 'context' }
    @{ n = 'fusionar y borrar la rama'; c = (ShC 'gh pr merge 90 --merge --delete-branch'); want = 'ask' }
    @{ n = 'commit con nombre vetado'; c = (ShC 'git commit -m "migra lo de Zanahoria"'); want = 'deny' }
    @{ n = 'PR con nombre vetado en camelCase'; c = (ShC 'gh pr create --title "x" --body "ZanahoriaErp"'); want = 'deny' }
    @{ n = 'grep de un nombre vetado'; c = (ShC 'git grep -n -i zanahoria'); want = 'allow' }
    # --- Otros repositorios ---
    @{ n = 'leer Karpo C#'; c = (PsC 'git -C C:\Git\Paranoia\Karpo show origin/main:README.md'); want = 'allow' }
    @{ n = 'copiar de Karpo C# al temporal'; c = (ShC 'cp /c/Git/Paranoia/Karpo/README.md "$TEMP/x.md"'); want = 'allow' }
    @{ n = 'commit en Theros'; c = (PsC 'git -C C:\Git\Paranoia\Theros commit -am x'); want = 'deny' }
    @{ n = 'borrar en Karpo C#'; c = (PsC 'Remove-Item C:\Git\Paranoia\Karpo\x.txt'); want = 'deny' }
    @{ n = 'escribir dentro del clon limpio'; c = (ShC 'echo x > C:/Git/Paranoia/karpo-fw-go/tmp.txt'); want = 'allow' }
    # --- Dependencias y formato ---
    @{ n = 'go get'; c = (ShC 'go get github.com/lib/pq'); want = 'ask' }
    @{ n = 'go mod tidy'; c = (ShC 'go mod tidy'); want = 'context' }
    @{ n = 'gofmt -w sobre carpeta'; c = (ShC 'gofmt -w contexts'); want = 'deny' }
    @{ n = 'gofmt -w sobre un fichero'; c = (ShC 'gofmt -w contexts/orders/module.go'); want = 'allow' }
    @{ n = 'gofmt -l'; c = (ShC 'gofmt -l pkg'); want = 'allow' }
    @{ n = 'batería'; c = (ShC 'go build ./... && go vet ./... && go test -count=1 ./...'); want = 'allow' }
    # --- Bases de datos e integración ---
    @{ n = 'base de Karpo C#'; c = (ShC 'psql "postgres://karpo:karpo@localhost:55432/karpo"'); want = 'deny' }
    @{ n = 'Supabase real'; c = (ShC 'psql "postgres://postgres:x@db.abcd.supabase.co:5432/postgres"'); want = 'deny' }
    @{ n = 'base remota'; c = (ShC 'KARPO_PG_DSN="postgres://u:p@10.0.0.5:5432/karpo" go test ./...'); want = 'ask' }
    @{ n = 'base de integración'; c = (ShC 'KARPO_PG_DSN="postgres://karpo:karpo@localhost:55473/karpo?sslmode=disable" go test ./...'); want = 'allow' }
    @{ n = 'suite sin slot'; c = (PsC './integration/run.ps1 -Down'); want = 'deny' }
    @{ n = 'suite en el slot 0'; c = (PsC './integration/run.ps1 -Slot 0 -Down'); want = 'deny' }
    @{ n = 'suite en un slot propio'; c = (PsC './integration/run.ps1 -Slot 61 -Down'); want = 'context' }
    @{ n = 'parar el slot 0'; c = (ShC 'docker compose -p integration down -v'); want = 'deny' }
    @{ n = 'parar el slot propio'; c = (ShC 'docker compose -p integration-s61 down -v'); want = 'context' }
    @{ n = 'liberar puertos'; c = (PsC 'net stop winnat'); want = 'deny' }
    # --- Ficheros ---
    @{ n = 'editar un documento'; c = (Ed "$R/docs/PEDIDOS.md" 'a' 'b'); want = 'allow' }
    @{ n = 'escribir en Theros'; c = (Wr 'C:\Git\Paranoia\Theros\x.md' 'x'); want = 'deny' }
    @{ n = 'escribir en el temporal'; c = (Wr "$env:TEMP\x.md" 'x'); want = 'allow' }
    @{ n = 'escribir fuera'; c = (Wr 'D:\otros\x.md' 'x'); want = 'ask' }
    @{ n = 'tocar go.mod'; c = (Ed "$R/go.mod" 'go 1.27.0' 'go 1.27.1'); want = 'ask' }
    @{ n = 'nombre vetado en un doc'; c = (Wr "$R/docs/x.md" 'Viene de Zanahoria.'); want = 'deny' }
    @{ n = 'palabra parecida'; c = (Wr "$R/docs/x.md" 'Las zanahorias van aparte.'); want = 'allow' }
    @{ n = 'clave privada'; c = (Wr "$R/x.pem" "-----BEGIN RSA PRIVATE KEY-----`nabc"); want = 'deny' }
    @{ n = 'dominio con dependencia'; c = (Wr "$R/pkg/domain/x.go" "package domain`n`nimport `"github.com/google/uuid`"`n"); want = 'ask' }
    @{ n = 'dominio con decimal'; c = (Wr "$R/pkg/domain/vocab/x.go" "package vocab`n`nimport (`n`t`"fmt`"`n`t`"github.com/shopspring/decimal`"`n`tfw `"github.com/jhermoso/karpo-fw-go/pkg/domain`"`n)`n"); want = 'allow' }
    @{ n = 'borrar una fila del registro'; c = (Ed "$R/docs/DECISIONES-COMUNES.md" "| D-140 | x |`n| D-141 | y |" '| D-141 | y |'); want = 'deny' }
    @{ n = 'arreglar un enlace del registro'; c = (Ed "$R/docs/DECISIONES-COMUNES.md" '| D-140 | [a](a.md) |' '| D-140 | [a](b.md) |'); want = 'context' }
    @{ n = 'migraciones'; c = (Ed "$R/contexts/orders/infrastructure/persistence.go" 'a' 'b'); want = 'context' }
    # --- Casos de la revisión independiente (2026-11-14) ---
    @{ n = 'push a main tras una lectura'; c = (ShC 'git status && git push origin HEAD:main'); want = 'deny' }
    @{ n = 'push forzado tras un log'; c = (ShC 'git log -1 ; git push --force origin x'); want = 'ask' }
    @{ n = 'gofmt -w por tubería'; c = (ShC 'git diff --name-only | xargs gofmt -w'); want = 'deny' }
    @{ n = 'push con -C'; c = (ShC "git -C `"$R`" push origin HEAD:main"); want = 'deny' }
    @{ n = 'push con -c'; c = (ShC 'git -c user.name=x push origin HEAD:main'); want = 'deny' }
    @{ n = 'commit con -C y nombre vetado'; c = (ShC "git -C $R commit -m 'Zanahoria'"); want = 'deny' }
    @{ n = 'commit -F con nombre vetado'; c = (ShC "git commit -F `"$env:KARPO_GUARD_MSG`""); want = 'deny' }
    @{ n = 'commit en PowerShell con here-string'; c = (PsC "git commit -m @'`nrun.ps1 sin -Slot`n'@"); want = 'allow' }
    @{ n = 'añadir run.ps1'; c = (ShC 'git add integration/run.ps1'); want = 'allow' }
    @{ n = 'leer run.ps1'; c = (ShC 'sed -n 1,20p integration/run.ps1'); want = 'allow' }
    @{ n = 'suite con -Slot:3'; c = (PsC './integration/run.ps1 -Slot:63 -Down'); want = 'context' }
    @{ n = 'suite en el slot 120'; c = (PsC './integration/run.ps1 -Slot 120'); want = 'deny' }
    @{ n = 'git -C sobre este checkout entre comillas'; c = (PsC 'git -C "C:\Git\Paranoia\Karpo.Fw.Go" checkout -b carril/y origin/main'); want = 'allow' }
    @{ n = 'grep de rm en Karpo C#'; c = (ShC 'grep -rn "rm " /c/Git/Paranoia/Karpo/docs'); want = 'allow' }
    @{ n = 'show de un fichero add-user'; c = (PsC 'git -C C:\Git\Paranoia\Karpo show origin/main:docs/add-user.md'); want = 'allow' }
    @{ n = 'copiar a Karpo C#'; c = (ShC 'cp README.md /c/Git/Paranoia/Karpo/README.md'); want = 'deny' }
    @{ n = 'compose con --project-name propio'; c = (ShC 'docker compose --project-name integration-s63 up -d'); want = 'allow' }
    @{ n = 'humo en PostgreSQL desechable'; c = (PsC 'docker run --rm -d --name humo -p 127.0.0.1:55532:5432 postgres:17'); want = 'allow' }
    @{ n = 'reescribir el registro sin una fila'; c = (Wr "$R/docs/DECISIONES-COMUNES.md" "# Registro`n| D-1 | x |`n"); want = 'deny' }
    @{ n = 'dominio de un contexto con dependencia'; c = (Wr "$R/contexts/orders/domain/x.go" "package domain`n`nimport `"github.com/google/uuid`"`n"); want = 'ask' }
    @{ n = 'dominio de un contexto puro'; c = (Wr "$R/contexts/orders/domain/x.go" "package domain`n`nimport (`n`t`"time`"`n`tfw `"github.com/jhermoso/karpo-fw-go/pkg/domain`"`n)`n"); want = 'allow' }
    @{ n = 'decimal en la raíz del dominio'; c = (Wr "$R/pkg/domain/x.go" "package domain`n`nimport `"github.com/shopspring/decimal`"`n"); want = 'ask' }
    @{ n = 'worktree con la raíz del proyecto en otro sitio'; c = (Wr "$R/pkg/domain/x.go" "package domain`n`nimport `"github.com/google/uuid`"`n"); want = 'ask'; project = 'C:/Git/Paranoia/Karpo.Fw.Go' }
)

$fail = 0
foreach ($k in $cases) {
    if ($k.ContainsKey('only') -and -not $k.only) { Write-Host ("omitido  {0}" -f $k.n); continue }
    $env:CLAUDE_PROJECT_DIR = if ($k.ContainsKey('project')) { $k.project } else { $root }
    $json = @{ tool_name = $k.c.tool; tool_input = $k.c.input; cwd = $root } | ConvertTo-Json -Compress -Depth 4
    $out = $json | pwsh -NoProfile -NonInteractive -File $guard
    $got = 'allow'
    if ($out) {
        $o = ($out | ConvertFrom-Json).hookSpecificOutput
        $got = if ($o.permissionDecision) { $o.permissionDecision } elseif ($o.additionalContext) { 'context' } else { 'allow' }
    }
    if ($got -eq $k.want) { Write-Host ("bien     {0}" -f $k.n) }
    else { $fail++; Write-Host ("MAL      {0}: esperaba {1}, salió {2} {3}" -f $k.n, $k.want, $got, $out) }
}
Write-Host ("{0} casos, {1} mal" -f $cases.Count, $fail)
exit ([int]($fail -gt 0))
