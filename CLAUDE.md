# CLAUDE.md — Karpo Go

Karpo en Go: el framework (`pkg/`), los contextos de negocio (`contexts/`) y el anfitrión
(`host/`). Traducción idiomática del framework de C#, sobre DDD, arquitectura limpia y CQRS.

## Proyectos hermanos

| Proyecto | Carpeta | Qué es |
|---|---|---|
| Trench | — | ERP original en C#; sigue evolucionando. Referencia del esquema de datos |
| Karpo C# | `C:\Git\Paranoia\Karpo` | Karpo en C# |
| Karpo Go | este repositorio | Karpo en Go |
| Theros | `C:\Git\Paranoia\Theros` | Generador de código; compila lo que genera contra este framework |

Cada proyecto tiene **una sola sesión de orquestación**. Desde ésta no se escribe en los
otros repositorios: se leen. Si hace falta un cambio allí, se anota y se pide a su sesión.

**Datos:** la estructura de las bases de datos debe mantenerse igual a la de Trench y
Karpo C# siempre que sea posible. Una diferencia se justifica por escrito; si lo que hay en
Trench es un error, se corrige aquí y se traslada a Trench.

## Arquitectura (la misma en los cuatro proyectos)

`Front → Distribution → Application → Domain ← Infrastructure`, más `Testing`.
El dominio no importa nada de las otras capas.

En este repositorio:

| Carpeta | Qué |
|---|---|
| `pkg/domain/` | Contratos de dominio: identidad, agregados, eventos, especificaciones, vocabulario, rasgos |
| `pkg/application/` | Contratos de aplicación: CQRS, cadena de permiso, idempotencia, transacción |
| `pkg/` (resto) | Persistencia agnóstica con un paquete por motor, distribución, mensajería |
| `contexts/<nombre>/` | Un contexto de negocio, con sus capas |
| `examples/parties/` | El «alfiler»: contexto de ejemplo, banco de pruebas de Theros |
| `host/`, `cmd/` | Composición y ejecutables |
| `integration/` | Pruebas contra motores reales, en contenedores |
| `docs/` | Un documento por contexto y por tema; empezar por `docs/ARQUITECTURA.md` |

## Comprobar

```
go build ./... && go vet ./... && go test -count=1 ./...
```

Las pruebas contra motores reales están en `integration/` y usan sus propios contenedores.
Lo pendiente se lleva en `BACKLOG.md`.

## Reglas fijas

- **Git:** un commit por tarea, en su rama. No se hace push ni se fusiona en la rama
  principal sin que Javier lo pida. Nunca se saltan los hooks.
- **Bases de datos:** tres motores (PostgreSQL, SQL Server, Oracle). Las pruebas van sobre
  bases nuevas y desechables; una base viva no se borra ni se recrea nunca.
- **Secretos:** ninguno en código ni en configuración.
- **El dominio es puro:** sólo biblioteca estándar.
- **Sencillez:** implementaciones muy sencillas, claras antes de escribir código.
- **Idioma:** documentación, comentarios y commits en español; identificadores en inglés.

## Cómo informar a Javier

- Informes cortos. Lo primero, el resultado; después, lo que no se ha ejecutado.
- Cada tramo termina con **un comando que él pueda ejecutar** y lo que debería ver.
- Como mucho tres decisiones por mensaje, y sólo las de producto o las irreversibles. Las
  técnicas y reversibles se toman, se anotan y se le cuentan.
- Los términos de herramientas ajenas se explican en una frase, con un ejemplo.
