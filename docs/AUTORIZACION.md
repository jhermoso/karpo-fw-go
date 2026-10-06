# Evaluación de los contratos de autorización y actor (Fw C# → Go)

Evaluación de `Fw.Application.Contracts/Authorization`, de sus implementaciones en ErpKernel y de
los middlewares de `Fw.Distribution`, frente al **contrato de autorización v1**
(`000-Docs/planning/seguridad-4.1/contrato-autorizacion-v1.md`) y a las decisiones de negocio ya
confirmadas (P1–P3, D7). Se usa el mismo método que en [LENGUAJE-UBICUO.md](LENGUAJE-UBICUO.md) y
[RASGOS-TRANSVERSALES.md](RASGOS-TRANSVERSALES.md).

## Método

| Criterio | Peso |
|---|---|
| **U** Utilidad (uso real en contextos, sin Fw ni tests) | 40 % |
| **C** Corrección de la implementación | 25 % |
| **D** Diseño de la firma | 20 % |
| **G** Encaje en Go | 15 % |

Puntuación de 1 a 5 por criterio; nota ponderada sobre 100. **Mantener** ≥ 70 · **Modificar** 45–69 ·
**Retirar** < 45 · **Sustituido**.

## Uso medido en C#

| Pieza | Uso |
|---|---|
| `RequirePermission` (políticas por endpoint) | 117 endpoints |
| `IOrganizationScopeProvider` | 159 archivos; `ScopedOrganizationIds` 112; `CanWrite` 5 |
| `AuthorizationContext` / `IAuthorizationContextResolver` | 1 resolvedor real (`SecurityAuthorizationContextResolver`) + modo HTTP y de test |
| `ICurrentActorResolver` | `JwtCurrentActorResolver` + `FwCurrentActorMiddleware` |
| `IRoleAuth`, `IdRoleAuth`, `IdUserAuth` (en `Domain.Contracts`) | uso residual |

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | `AuthorizationContext` (sujeto, tipo de actor, party, admin global, grants, permisos, ámbito pedido y efectivo, versión de política, correlación) | 5 | 4 | 4 | 4 | **88** | Mantener → `authz.Context` + `authz.NewContext` (valida las invariantes) |
| 2 | `PermissionCodes` (`{Subdominio}.{Recurso}.{Acción}`, comodín `*.*.*`) | 5 | 4 | 4 | 5 | **89** | Mantener → `authz.Permission`, `PermissionOf`, `MustPermission`, `Wildcard` |
| 3 | `OrganizationGrant` + `OrganizationAccessLevel` (`Full`/`ReadOnly`/`Restricted`, desconocido → `Restricted`) | 5 | 4 | 4 | 5 | **89** | Mantener → `authz.Grant`, `AccessLevel`, `ParseAccessLevel`; P2 en `CanWrite` |
| 4 | `AuthorizationOutcome` + `AuthorizationResolution` (`Allow` ⇔ contexto) | 4 | 5 | 5 | 4 | **88** | Mantener → `authz.Resolution` (`Allowed`/`Denied`/`Undetermined`, `Err()`) |
| 5 | `PolicyVersionHasher` | 3 | 5 | 4 | 5 | **80** | Mantener → `authz.PolicyVersion` |
| 6 | Principales de servicio por configuración (sin comodín, sin admin global) | 3 | 5 | 4 | 4 | **78** | Mantener → `authz.ServicePrincipal`, `ServicePrincipalContext` |
| 7 | `AddFwJwtAuthentication` (HS256, emisor, audiencia, 1 min de margen) | 5 | 4 | 3 | 3 | **80** | Mantener → `distribution/jwtauth` (solo biblioteca estándar) |
| 8 | `SecurityAuthorizationContextResolver` | 5 | 4 | 3 | 3 | **80** | Modificar → `authorization.Resolver` genérico sobre el puerto `authz.Directory` |
| 9 | `IAuthorizationContextResolver.ResolveAsync(ClaimsPrincipal, …)` | 5 | 4 | 3 | 2 | **77** | Modificar → `authz.Authenticator` (credenciales → `Principal`) + `authz.Resolver` (`Principal` → `Resolution`) |
| 10 | `IOrganizationScopeProvider` (`ScopedOrganizationIds`, `CanWrite`) | 5 | 2 | 3 | 2 | **66** | Modificar → `authz.ScopeSpec` (el filtro se traduce a SQL) + `Context.CanWrite` / `authz.RequireWrite` |
| 11 | Políticas `RequirePermission` en el endpoint | 5 | 3 | 3 | 2 | **70** | Modificar → `pipeline.RequirePermission` en el caso de uso; `distribution.RequirePermission` solo para endpoints sin caso de uso |
| 12 | `FwAuthorizationContextMiddleware` (401/403/503, `X-Organization-Scope`) | 5 | 3 | 3 | 3 | **74** | Mantener → `distribution.Authorize` |
| 13 | Modos `Local` / `Http` / `Legacy` / `Test` | 3 | 2 | 2 | 2 | **50** | Modificar: `Legacy` desaparece; `Test` = `authorization.MemoryDirectory`; `Http` será otra implementación de `authz.Resolver` (pendiente) |
| 14 | `IAuthorizationContextAccessor` (servicio con ámbito de petición) | 5 | 4 | 3 | 1 | 71 | Sustituido → `authz.WithContext` / `authz.FromContext` en `context.Context` |
| 15 | `ICurrentActorResolver`, `JwtCurrentActorResolver`, `FwCurrentActorMiddleware` | 4 | 2 | 2 | 2 | 56 | Sustituido → el actor se deriva del contexto resuelto (`Context.Actor()` → `application.WithActor`) |
| 16 | `IUserOrganizationAccessResolver` + `UserOrganizationAccessResolver` | 3 | 3 | 2 | 2 | 53 | Sustituido → los grants ya viajan en `authz.Context` |
| 17 | `FwOrganizationScopeMiddleware` (legado) | 2 | 2 | 1 | 1 | **34** | Retirar |
| 18 | `IRoleAuth`, `IdRoleAuth`, `IdUserAuth` | 1 | 3 | 2 | 2 | **38** | Retirar (los roles solo sirven para calcular admin global y versión de política) |
| 19 | `IAuthorizable` en la entidad | — | — | — | — | 28 | Ya retirado en [RASGOS-TRANSVERSALES.md](RASGOS-TRANSVERSALES.md) |

## Defectos encontrados (justifican la columna C)

- **El permiso solo se comprueba en el borde HTTP.** `RequirePermission` es una política de
  endpoint: los trabajos programados, los consumidores de mensajes y las llamadas internas a los
  casos de uso no pasan por ella. En Go, `pipeline.RequirePermission` decora el propio caso de uso.
- **El nivel de acceso casi no se comprueba al escribir.** 159 archivos filtran por ámbito, pero
  solo 5 consultan `CanWrite`. Una organización `ReadOnly` o `Restricted` del ámbito efectivo
  puede aceptar escrituras en todos los casos de uso que no llaman a `CanWrite` (hay que
  revisarlos uno a uno en C#). En Go, `authz.RequireWrite(ctx, org)` es la comprobación explícita,
  y `CanWrite` exige además que la organización esté dentro del ámbito pedido.
- **El modo `Legacy` falla abierto.** Si el principal no tiene `partyId` o `username`, se audita
  como `System` y la petición sigue. En Go no hay modo `Legacy`: sin contexto, `authz.Require`
  devuelve `ErrUnauthorized`, y `authz.ScopeSpec` no devuelve ninguna fila.
- **Hay dos middlewares y dos resolvedores que leen las mismas cabeceras y claims.**
  `FwAuthorizationContextMiddleware`/`FwOrganizationScopeMiddleware` analizan cada uno
  `X-Organization-Scope`, y `JwtCurrentActorResolver`/`SecurityAuthorizationContextResolver`
  leen cada uno `sub`, `partyId` y `username`. Pueden divergir. En Go, `distribution.Authorize`
  es el único punto: autentica, resuelve y deriva el actor.
- **El actor puede venir de una cabecera del cliente.** `distribution.TenantActorContext`
  (`X-Actor-ID`, `X-Actor-Name`) sirve para un gateway de confianza o para desarrollo.
  `Authorize` ignora esas cabeceras y sella la auditoría con el sujeto resuelto; hay un test que
  lo comprueba.

## Diseño en Go

```text
Authorization: Bearer …  ─►  authz.Authenticator  ─►  authz.Principal
                              (jwtauth.HS256)          │
X-Organization-Scope  ────────────────────────────────►  authz.Resolver  ─►  authz.Resolution
                                                         (authorization.Resolver     Allow / Deny / Indeterminate
                                                          sobre authz.Directory)
distribution.Authorize:  Deny incompleto → 401 · Deny → 403 · Indeterminate → 503 + Retry-After: 5
                         Allow → authz.WithContext + application.WithActor
casos de uso:            pipeline.RequirePermission[In,Out]("Parties.Party.Create")
                         authz.RequireWrite(ctx, org) · authz.ScopeSpec(ctx, FieldOwner, wrap)
```

| Paquete | Qué contiene |
|---|---|
| `pkg/application/authz` (contrato) | `Principal`, `Authenticator`, `Resolver`, `Request`, `Resolution`, `Context`, `Grant`, `AccessLevel`, `Permission`, `ServicePrincipal`, `Directory`/`Subject`, `Require`, `RequireWrite`, `ScopeSpec`, `PolicyVersion` |
| `pkg/application/authorization` | `Resolver` genérico (usuario desconocido, inactivo o bloqueado, principales de servicio, `Indeterminate` si falla el directorio) y `MemoryDirectory` |
| `pkg/application/pipeline` | `RequirePermission` |
| `pkg/distribution` | `Authorize`, `RequirePermission`, `OrganizationScopeHeader`; `ErrIndeterminate` → 503 |
| `pkg/distribution/jwtauth` | `HS256` (`Authenticate`, `Issue`): rechaza `alg: none`, firmas ajenas, emisor y audiencia incorrectos, caducados y sin `exp` |

El contexto de Security implementa `authz.Directory` sobre sus repositorios (usuarios, roles,
permisos y accesos a organizaciones; ver [SEGURIDAD.md](SEGURIDAD.md)). El resolvedor no guarda
caché entre peticiones (D7).

## Validación

- Contratos: invariantes de `NewContext`, códigos de permiso, comodín, admin global, niveles de
  acceso según P2 (incluido «`Full` fuera del ámbito pedido no escribe»), `Require`,
  `RequireWrite`, principales de servicio sin comodín, versión de política determinista y
  `ScopeSpec` (sin contexto no devuelve nada; el admin global lo ve todo).
- Resolvedor: todos los resultados (`incomplete-principal`, `unknown-subject`, `inactive-user`,
  `locked-user`, `unknown-service-principal`, `Allow`) e `Indeterminate` cuando falla el directorio.
- JWT: token válido, margen de reloj, caducado, sin `exp`, firma ajena, audiencia, `alg: none`,
  manipulado y vacío.
- Extremo a extremo HTTP: JWT → resolvedor → `Authorize` → caso de uso con
  `pipeline.RequirePermission` y `RequireWrite`. Se comprueban 401 (con `WWW-Authenticate`), 400,
  403, 201 y 503 con `Retry-After`, y que el actor es el sujeto resuelto aunque el cliente envíe
  `X-Actor-Name`.
- `archtest`: `authz` es un paquete de contrato; `authorization` solo depende de contratos; la
  persistencia no depende de `authorization`.

## Pendiente

- Resolvedor en modo `Http` (consultar el contexto a Security por red, sobre su
  `GET /api/auth/context`).
- ~~Implementar `authz.Directory` en el contexto Security~~: hecho, ver
  [SEGURIDAD.md](SEGURIDAD.md). `security.Module.Directory` sustituye a `MemoryDirectory`, y
  `authorization.Authenticators` combina el token propio con el de un proveedor de identidad externo.
- ~~Visibilidad de Parties por `PartyRelationship` + permiso (P1)~~: hecha en
  [PARTIES.md](PARTIES.md). ~~`OrganizationAdmin` con ámbito (P3)~~: hecho en Security.
- `IncludeSubsidiaries` viaja en el contrato, pero en v1 no amplía el ámbito (P2).
