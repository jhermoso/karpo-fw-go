# Publicadora de humo

Una «publicadora de humo» es **un ejecutable aparte** que sirve lo mismo que `cmd/karpo` y
añade una **puerta por la que se entra sin contraseña**. Sirve para probar el conjunto desde un
navegador o con `curl` sin que nadie lea y teclee una credencial. **Sólo existe en desarrollo.**

| Qué | Dónde |
|---|---|
| La puerta | `host/smoke/` |
| El ejecutable | `cmd/karpo-smoke/` |
| La regla que la vigila | `pkg/testing/archtest/smoke_test.go` (reglas 10 y 11) |

## Arrancarla y entrar

```powershell
$env:KARPO_ENV = 'development'
go run -tags smoke ./cmd/karpo-smoke -port 8092 -seed
```

| Opción | Qué hace |
|---|---|
| `-port` | Puerto de `127.0.0.1` (por defecto 8092). No se puede elegir otra dirección |
| `-db` | Fichero SQLite, **nuevo** (por defecto, uno en una carpeta temporal nueva; la ruta sale al arrancar) |
| `-seed` | Carga una organización en Parties si no hay ninguna |

```powershell
curl.exe -s "http://127.0.0.1:8092/api/smoke/token?as=admin"        # la sesión en JSON
# con el accessToken de esa respuesta:
curl.exe -s http://127.0.0.1:8092/api/parties -H "Authorization: Bearer <accessToken>"
```

Debería verse: un aviso en mayúsculas de que no hay autenticación, la ruta de la base, y en la
primera orden un JSON con `"success":true` y `"username":"smoke-admin"`.

## Rutas (el contrato común de las cuatro publicadoras de humo)

| Ruta | Qué hace |
|---|---|
| `GET /api/smoke/enter?as=<perfil>` | Página que guarda en `sessionStorage` lo que el front guarda tras un login (`auth-session`, `auth-token`, `auth-refresh-token`) y salta a `/`. Karpo Go no tiene front: está para que la puerta sea la misma en los cuatro proyectos |
| `GET /api/smoke/token?as=<perfil>` | La misma sesión en JSON, para `curl` |
| `POST /api/auth/login` | Para los perfiles fijos, cualquier contraseña vale. Para cualquier otro usuario es el login de siempre |

Todo lo demás es el anfitrión tal cual: sus rutas, sus permisos, `/api/auth/refresh`, `/healthz` y `/readyz`.

## Quién entra

Un perfil, fijo en el código (`host/smoke/smoke.go`, `Profiles`):

| Perfil | Usuario | Qué puede |
|---|---|---|
| `admin` | `smoke-admin` | Es el administrador global de la instalación: todo |

El nombre lleva «smoke» para que en la auditoría (`createdBy`) y en las trazas se vea que no es
una persona.

## Sin secretos

- La **clave que firma las sesiones** se genera al arrancar (32 bytes aleatorios) y vive en la
  memoria del proceso. No se lee `KARPO_JWT_SECRET`.
- La **contraseña de `smoke-admin`** también se genera al arrancar, no se imprime ni se guarda.
  La publicadora la cambia una vez ella misma, para que el anfitrión no exija cambiarla antes
  de nada (el 403 del primer inicio de sesión).
- Se **niega a arrancar** si `KARPO_BOOTSTRAP_ADMIN_USER` o `KARPO_BOOTSTRAP_ADMIN_PASSWORD`
  están puestas: no acepta credenciales de nadie.
- Consecuencia: **sólo arranca sobre una base que aún no tiene administrador**. Al parar, nadie
  conoce la contraseña del que creó; un segundo arranque sobre el mismo fichero se niega. Cada
  arranque, base nueva.

## Las tres barreras

| Barrera | Cómo | Prueba |
|---|---|---|
| **Compilación** | Todos sus ficheros llevan `//go:build smoke`. Sin `-tags smoke` no está en ningún binario y `cmd/karpo-smoke` no compila | `go build ./cmd/karpo-smoke` falla con «build constraints exclude all Go files» |
| **Arranque** | Exige `KARPO_ENV` **exactamente** `development` (si falta, se niega) y sólo escucha en `127.0.0.1`, `::1` o `localhost` | `TestNew_RefusesOutsideDevelopment`, `TestListen_RefusesWhatIsNotThisMachine`, `TestRun_RefusesOutsideDevelopment` (con la etiqueta) |
| **Pipeline** | Una prueba **sin etiqueta**, que corre siempre, falla si un `.go` sin la etiqueta importa `host/smoke` o nombra sus rutas, o si un fichero de la publicadora pierde la etiqueta | `TestSmoke_IsNotImportedByThePrograms`, `TestSmoke_StaysBehindItsTag`, y `TestSmoke_TheRuleBites`, que comprueba que la regla muerde |

## Comprobar

```powershell
go test -count=1 ./pkg/testing/archtest/...                               # la regla, sin etiqueta
go vet -tags smoke ./...
go test -tags smoke -count=1 ./host/smoke/... ./cmd/karpo-smoke/...       # la publicadora
```

Debería verse `ok` en las tres líneas de paquete. El CI ejecuta lo mismo (`.github/workflows/ci.yml`).
