## B-15: filtro `ids`, vuelta 2

**Resultado:** los cinco hallazgos del juez están resueltos y no he cambiado código de producción. En `feat/filtro-ids` hay tres commits sin push: `eb8577d` (el filtro), `644bf21` (correcciones de la vuelta 2) y `832b6ee` (la consulta 3 en la memoria de trabajo). `.claude/agents/` y `docs/FORMA-DE-TRABAJO.md` siguen fuera del commit.

### Hallazgos del juez
1. **Orden entre el 403 y el 400** (`docs/CONTRATO-MANTENIMIENTO.md`): el documento ya describe el orden real.
   - `type` (en `POST`) sale en distribución, antes del permiso.
   - En países, `format` y `range` también salen antes del permiso, igual que `eu` o `eurozone`.
   - En las demás listas, `format` y `range` salen en la aplicación, después del permiso.
   - No he movido la comprobación.
2. **Países sin permiso** (`contexts/geography/maintenance_test.go`, desde la línea 278): hay tres casos nuevos.
   - `?ids=` sin permiso da 403.
   - `?ids=ESP` sin permiso da 400 `ids/format`.
   - `POST` con `{"ids":[1]}` sin permiso da 400 `ids/type`.
3. **Cobertura:** `BACKLOG.md` tiene la entrada de testing con las cifras medidas contra `origin/main`: `hr/application` pasa de 72,9 a 73,0 % y `products/application` de 77,3 a 77,6 %. Ninguna de las dos baja.
4. **Mutantes vivos en código anterior:** `BACKLOG.md` los lista uno a uno, por dominio, con fichero, línea y tipo de mutación.
   - Los saqué con un mutador propio, porque gremlins v0.5.0 en Windows da todos los mutantes por tiempo agotado.
   - Consta que el filtro `ids` no depende de ninguno.
5. **Memoria de trabajo:** existe `docs/trabajo/b15-filtro-ids/` con `02-plan.md`, `03-construccion.md`, `consultas.md` y `veredictos.md`.
   - `consultas.md` recoge las consultas 1 y 2 y ahora también la 3: no consta que Javier haya confirmado las lecturas, así que se siguen como provisionales.
   - `veredictos.md` dice que las dos lecturas de §5 están pendientes de Javier.

### Comprobado ahora
- `go test -count=1 ./contexts/geography/...` → `ok` en todos los paquetes con pruebas.
- En esta vuelta no he repetido la batería completa ni la integración. El código de producción no ha cambiado desde la vuelta en la que pasaron: 91 paquetes en verde, y el slot 61 en los cinco motores, que luego borré con `down -v`.

### Comando para Javier
`cd C:/Git/Paranoia/karpo-fw-go-ids; go test -count=1 ./contexts/geography/`
Debería ver `ok`, ya con los casos de países sin permiso.

### Lo que no se ha ejecutado
- La suite de integración entera.
- Gremlins sobre las capas de aplicación y de distribución de cada contexto.
- Los avisos al carril del front (para que marque B-15) y a la sesión de Theros.
- La carpeta temporal bloqueada `C:/Users/Javier/AppData/Local/Temp/gremlins-4075944370` sigue ahí; se puede borrar a mano.

### Decisiones para Javier, antes de fusionar (las dos lecturas provisionales del asesor)
1. **¿Vale el «trinquete»?** Es decir: ¿puede un paquete tocado quedar por debajo del 80 % si no baja respecto a `origin/main` y tiene su entrada de testing en `BACKLOG.md`? La alternativa es subirlo al 80 % en la misma rama.
2. **¿Cuenta como «explicado» un mutante vivo en código anterior?** Las condiciones son tres: que esté listado uno a uno en `BACKLOG.md`, que se declare la herramienta (aquí, el mutador propio en lugar de gremlins) y que el cambio no dependa de él. La alternativa es matarlo en la misma rama.

Si Javier confirma, la sesión de orquestación lleva las dos lecturas a §5 de `FORMA-DE-TRABAJO.md` en otro commit.

### Decisiones técnicas ya tomadas (reversibles)
- El tope queda en 1000, sin bajarlo a 900.
- Los errorCodes son `type`, `format` y `range`; la identidad cero da `format`.
- En suppliers, pedir por `ids` o por `supplier=` trae también las fusionadas.
