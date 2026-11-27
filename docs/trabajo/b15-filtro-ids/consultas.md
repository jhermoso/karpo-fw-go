# B-15: consultas al asesor

Las dos consultas las hizo el juez de construcción en la vuelta 1, sobre cómo leer §5 de
`docs/FORMA-DE-TRABAJO.md`. Las respuestas se recogen tal como el juez las transmitió al
constructor; no son el texto literal del asesor.

## 1. El 80 % de cobertura en paquetes que ya estaban por debajo

**Pregunta.** Dos paquetes tocados quedan por debajo del 80 % por código anterior a la tarea
(`hr/application` y `products/application`). ¿Bloquea la aprobación?

**Respuesta.** Rige un «trinquete»: un paquete tocado puede quedar por debajo del 80 % si no baja
respecto a `origin/main`, siempre que `BACKLOG.md` lleve una entrada de tipo testing con el paquete
y su cifra.

## 2. Mutantes vivos en código anterior de los dominios tocados

**Pregunta.** Las líneas nuevas del dominio no generan mutantes, pero en los dominios tocados
sobreviven mutantes de código anterior. ¿Cuentan como «sin explicar»?

**Respuesta.** Cuentan como explicados sólo si se cumplen tres cosas:

1. `BACKLOG.md` los lista uno a uno, con fichero, línea y tipo de mutación, como tarea de testing;
2. se dice con qué herramienta se obtuvieron (gremlins en Windows da tiempo agotado en lugar de
   muertos);
3. consta que el filtro `ids` no depende de ninguna de esas condiciones.

Las dos lecturas están pendientes de que Javier las confirme (ver `veredictos.md`).

## 3. ¿Confirma Javier las dos lecturas? (constructor, vuelta 2)

**Respuesta del asesor.** No consta confirmación; el asesor no puede darla. Se sigue con las
lecturas como provisionales y se elevan a Javier en el informe final, antes de fusionar. Si las
confirma, la sesión de orquestación las lleva a §5 de `docs/FORMA-DE-TRABAJO.md` en otro commit.
La medida de mutación usa un mutador propio en lugar de gremlins (que en Windows da todo por
tiempo agotado); consta como dato de la segunda pregunta.
