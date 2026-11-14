# CLAUDE.md — `integration/` (pruebas contra servidores reales)

Módulo aparte (`integration/go.mod`, para que los drivers no entren en el framework). Cada prueba
de contexto borra y vuelve a migrar sus tablas, incluido el historial de migraciones, en cinco
servidores de Docker: PostgreSQL, Supabase, SQL Server, Oracle y MySQL.

- **Siempre en un slot propio:** `./integration/run.ps1 -Slot N -Down` (PowerShell, Docker). Un
  slot es un proyecto de Compose propio (`integration-sN`) con los puertos desplazados 10·N. N va
  de 1 a 99 (con 100 o más, Supabase pisa el PostgreSQL de otros slots); el 0, el de un `run.ps1`
  sin `-Slot`, no se usa porque es el que arrancaría cualquier otra sesión o Javier. Dos
  ejecuciones sobre los mismos servidores se rompen entre sí; un *deadlock* de MySQL o un historial
  de migraciones que desaparece a mitad de prueba suelen ser eso, no el código.
- **Elegir N:** uno sin contenedores (`docker ps --filter label=com.docker.compose.project=integration-sN`)
  y con los cinco puertos libres y fuera de los rangos que Windows reserva tras un reinicio
  (`netsh interface ipv4 show excludedportrange protocol=tcp`): PostgreSQL 55433, Supabase 54433,
  SQL Server 51433, Oracle 51521 y MySQL 53306, cada uno +10·N. Si todo sale «unreachable», busca
  «ports are not available» en la salida. Liberar la reserva (`net stop winnat`) es un cambio del
  sistema: se le deja a Javier.
- **Sólo se paran o borran los contenedores del proyecto que arrancó esta sesión.** Si una
  ejecución propia se interrumpió y dejó su slot ocupado: `docker compose -p integration-sN down -v`.
- **Una parte:** `-Run <patrón>` (por ejemplo `-Run TestProductsAndInventoryContexts`). La suite
  entera tarda entre 40 y 60 minutos: en segundo plano.
- Las contraseñas de `docker-compose.yml` y de `run.ps1` son de prueba; no se copian a ningún otro
  sitio.
