# CLAUDE.md — `pkg/` (el framework)

Lo que usan los 25 contextos, el anfitrión y el código que genera Theros. Un cambio aquí llega a
todos ellos.

- **Las dependencias entre paquetes las fija `pkg/testing/archtest`** (reglas 1 a 9; la 10 y la 11
  son de la publicadora de humo). Las más fáciles de romper:
  - `pkg/domain` y `pkg/domain/spec`: sólo biblioteca estándar; el resto del árbol del dominio
    admite además `github.com/shopspring/decimal`. Nunca `database/sql`, `net/http` ni `unsafe`.
  - Los contratos de aplicación (`pkg/application`, `authz`) sólo importan contratos; las
    implementaciones (`pipeline`, `orchestration`, `outbox`, `hosting`, `authorization`,
    `messaging`) tampoco tocan adaptadores ni código de terceros.
  - `pkg/persistence` no importa drivers: cada motor es un dialecto en `sqlrepo/<motor>` que recibe
    un `*sql.DB` ya abierto.
  - OpenTelemetry no entra nunca (regla 9; si llega, irá en un módulo `otel/` aparte).
  Si una regla estorba, se pregunta antes de cambiarla.
- **API pública:** renombrar, quitar o cambiar la firma de algo exportado rompe contextos y lo que
  genera Theros. Se busca su uso en `contexts/`, `host/`, `cmd/`, `examples/` e `integration/` y se
  avisa a «Orquestador Theros» en la PR.
- **Persistencia:** un cambio en `sqlrepo` o en un dialecto se prueba con la batería de
  conformidad (`pkg/testing/repotest`, `sqlrepo/sqlconformance`) y con la suite de
  `integration/` en los cinco motores. Un cambio en cómo se renderiza un tipo cambia el checksum
  de las migraciones ya aplicadas (`docs/ESQUEMA-MIGRACIONES.md`): necesita una decisión aparte.
- **Un patrón nuevo o retirado** se anota en `docs/INVENTARIO-PATRONES.md`; uno de observabilidad,
  en `docs/OBSERVABILIDAD.md`.
