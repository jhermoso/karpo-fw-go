# Prompt: corregir los value objects del lenguaje ubicuo del Fw C# de Karpo

> Para usar en una sesión de Claude Code abierta en `C:\Git\Paranoia\Karpo` (backend en
> `020-Back`). Origen: la evaluación `docs/LENGUAJE-UBICUO.md` de Karpo.Fw.Go.

---

Corrige los defectos de los value objects del lenguaje ubicuo del framework C# de Karpo
(`020-Back/020-Source/Paranoia.Karpo.Fw.Domain.Contracts/LenguajeUbicuo` y
`020-Back/020-Source/Paranoia.Karpo.Fw.Domain/LenguajeUbicuo`). Los defectos ya están
diagnosticados; abajo tienes cada uno con su fichero, el problema y la corrección esperada.

## Reglas de trabajo

1. Sigue `020-Back/CLAUDE.md`. Crea una rama `fix/fw-value-objects` desde la rama principal.
2. **Primero reproduce, luego corrige**: para cada defecto escribe antes un test que falle
   (`Paranoia.Karpo.Fw.Domain.Test` o `Paranoia.Karpo.Fw.Domain.Contracts.Test`, según dónde viva
   el tipo). Después corrige y comprueba que pasa. No cambies nada sin haber confirmado el defecto
   con un test.
3. **No cambies formatos persistidos sin migración.** Antes de tocar la normalización o el formato
   de un valor, busca sus conversiones EF (`HasConversion`, `EfConfiguration`) y los scripts de
   seed de **Oracle y SQL Server**. Si el cambio altera lo que se guarda, detente y explícalo en
   vez de aplicarlo.
4. Si un cambio rompe la API pública (firma, igualdad, excepciones), localiza **todos** los usos en
   `020-Source` (ErpKernel, ErpDetail, AdvisoryKernel, FinancialKernel, MaccorpKernel...) y
   adáptalos en el mismo cambio. Enumera los cambios que rompen compatibilidad en el resumen final.
5. Al terminar: `dotnet build 020-Source/Karpo.sln` y `dotnet test 020-Source/Karpo.sln`, sin
   nuevos fallos. Si algún test ya fallaba antes de empezar, indícalo.
6. Haz un commit por value object (o por grupo muy relacionado), con mensajes claros.

## Defectos a corregir (por prioridad)

### A. Errores de comportamiento

1. **`Telephone`** — `Fw.Domain/LenguajeUbicuo/SustantivosComunes/Telephone/Telephone.cs`
   - Al quitar los separadores, la expresión `^\+?([0-9]{1,3})[\s.-]?([0-9]{1,14})$` toma con
     avidez 3 dígitos como prefijo: `"+34 600 111 222"` da país **346** y número `00111222`.
   - Corrección: identifica el prefijo de país con la tabla de prefijos ITU-T E.164, que es un
     conjunto libre de prefijos: basta con probar 1, 2 y 3 dígitos contra la tabla. Admite `00` como
     equivalente de `+`. Si no hay prefijo internacional, exige que se indique el país (añade un
     constructor `Telephone(string number, string defaultCountryCode)`).
   - `Equals(ITelephone)` lanza `NotImplementedException`: impleméntalo comparando `Value`.
   - Tests mínimos: `+34 600 111 222` → 34/600111222; `0034600111222` → 34; `+1 212 555 0100` → 1;
     `+44 7911 123456` → 44; `+351 912 345 678` → 351.

2. **CIF en `IdentificationNumber`** —
   `Fw.Domain/LenguajeUbicuo/SustantivosComunes/IdentificationType/DocumentIdentificationType.cs`,
   `ValidateSpanishCif`
   - La letra de control se calcula como `'A' + (controlValue - 1)`, que para control 0 da `'@'`.
     La tabla correcta es `"JABCDEFGHI"[controlValue]`.
   - Aplica también la regla del tipo de entidad: `P, Q, R, S, N, W` exigen letra de control;
     `A, B, E, H` exigen dígito; el resto admiten ambos.
   - Tests con CIF válidos e inválidos de cada caso, incluido uno con control 0 (letra `J`).

3. **`SocialSecurityNumber`** — `Fw.Domain/LenguajeUbicuo/SustantivosComunes/SocialSecurityNumber.cs`
   - Valida el formato de EE. UU. (`XXX-XX-XXXX`). El NSS español tiene 12 dígitos:
     provincia (2) + número (8) + control (2). Control = `valor mod 97`, donde `valor` es
     `provincia * 10^7 + número` si `número < 10.000.000`, y la concatenación `provincia‖número`
     en otro caso.
   - Alinea con esa regla la validación de `DocumentIdentificationType.SocialSecurity`
     (hoy `^\d{11,12}$`).
   - Antes de cambiarlo, revisa los 3 usos en contextos y si hay NSS persistidos o en seeds.

4. **`PassportNumber`** — `Fw.Domain/LenguajeUbicuo/SustantivosComunes/PassportNumber.cs`
   - Valida `^[A-Z0-9]+$` **antes** de pasar a mayúsculas, así que rechaza `"ab123456"`.
     Normaliza (trim + mayúsculas) antes de validar.

5. **`UTCDateTime`** — `Fw.Domain.Contracts/LenguajeUbicuo/Sustantivos/UtcDateTime/UTCDateTime.cs`
   - El constructor y `FromDateTime` hacen `DateTime.SpecifyKind(x, Utc)` cuando `Kind != Utc`:
     una hora **local** queda etiquetada como UTC sin convertirse (desplazamiento silencioso).
     Corrección: si `Kind == Local`, usa `ToUniversalTime()`. Si `Kind == Unspecified`, mantén el
     comportamiento actual, porque es lo que devuelve EF al materializar, y documéntalo.
   - La igualdad incluye `OffsetFromLocal` (el offset horario del **servidor**): dos instantes
     idénticos creados en servidores distintos no son iguales. La igualdad debe comparar solo el
     instante (`Value`). Marca `OffsetFromLocal` como `[Obsolete]` sin eliminarlo.

6. **`ExpirationDate`** —
   `Fw.Domain.Contracts/LenguajeUbicuo/Adjetivos/Contratos_y_Componentes/Expirable/ExpirationComponnet/ExpirationDate.cs`
   - `_minValidDate` y `_maxValidDate` son `static readonly` calculados con `DateTime.UtcNow`: se
     congelan al arrancar el proceso. Calcúlalos en cada validación.
   - `Never` devuelve `UtcNow.AddYears(50)`, un valor distinto en cada llamada. Evalúa sustituirlo
     por una constante centinela y añadir `IsNever`. **No lo apliques** si cambia valores ya
     persistidos o comparaciones existentes; en ese caso documenta el impacto y propón la
     migración.
   - El operador `explicit operator ExpirationDate(DateTime)` valida «futuro». Revisa todas las
     conversiones EF y los puntos de rehidratación: deben usar `UnsafeRehydrate`, nunca el operador
     ni el constructor público.

7. **`ValidPeriod` y `Milestone`** — `Fw.Domain/.../ValidPeriod/ValidPeriod.cs`,
   `Fw.Domain.Contracts/.../Init/Milestone.cs`
   - Usan `DateTime.UtcNow` directamente, así que no se pueden probar e ignoran `IClock`. Añade
     sobrecargas que reciban el instante de referencia (`IsActiveAt(DateTime nowUtc)`,
     `Elapsed(DateTime nowUtc)`...) y haz que las actuales deleguen en ellas.
   - `Milestone.ElapsedYears` compara `DayOfYear` y falla en años bisiestos (p. ej. del 1-mar de
     un año bisiesto al 1-mar siguiente). Compara `(Month, Day)`.
   - `ValidPeriod`: normaliza `StartDate`/`EndDate` a UTC igual que `Milestone`.

8. **`Percentage`** — `Fw.Domain/LenguajeUbicuo/SustantivosComunes/Percentage/Percentage.cs`
   - Los operadores `+` y `-` recortan a 0/100 **en silencio** y ocultan errores de cálculo. Deben
     lanzar la misma excepción que el constructor si el resultado sale del rango.

9. **`PostalCode`** — `Fw.Domain/LenguajeUbicuo/SustantivosComunes/PostalCode/PostalCode.cs`
   - `Value` guarda la entrada tal cual y `FormattedValue` la normaliza, así que `"sw1a1aa"` y
     `"SW1A 1AA"` no son iguales. Normaliza `Value` (mayúsculas, sin espacios) para GB, NL y CA.
     Comprueba antes las conversiones EF: hoy no hay construcciones del VO en contextos, pero
     verifica que no haya datos persistidos a través de él.

### B. Implementaciones incompletas (`NotImplementedException`)

10. **`Email`** — hay **dos** tipos `Email` duplicados:
    `Fw.Domain.Contracts/LenguajeUbicuo/Sustantivos/Email/Email.cs` y
    `Fw.Domain/LenguajeUbicuo/SustantivosComunes/Email/Email.cs`. El de Contracts lanza
    `NotImplementedException` en `Equals(IEmail)`.
    - Deja uno solo (el de `Fw.Domain.Contracts`, junto a `IEmail`), con la implementación
      correcta de `Equals`, y elimina el otro. Adapta `ErpKernel.Domain/.../EmailContact.cs` y los
      demás usos.
    - `Value` se pasa a minúsculas pero `LocalPart` y `Domain` conservan las mayúsculas originales:
      derívalos del valor normalizado. No cambies la normalización de `Value`, porque hay datos
      persistidos y restricciones de unicidad que dependen de ella.

11. **`DateValue.Equals(IDateValue)`**, **`PersonalName.UpdateCurrentName`** e
    **`Icon.UpdateCurrentName`** lanzan `NotImplementedException`. Implementa `Equals`. Elimina los
    dos `UpdateCurrentName` si no forman parte de ningún contrato (compruébalo) o impleméntalos.

### C. Diseño (con cuidado: `Name` tiene 125 construcciones)

12. **`Name`** — `Fw.Domain.Contracts/LenguajeUbicuo/Sustantivos/Name/Name.cs`
    - El `Format` forma parte de la igualdad: `new Name("Acme", Normal) != new Name("Acme", FirstCapitalized)`.
      La igualdad debe basarse solo en `Value`. Antes, confirma que ningún uso depende de
      distinguir el formato.
    - `NameFormat.AllCapitalized` rechaza nombres reales con partículas en minúscula
      («Juan de la Fuente»). Admite las partículas `de, del, la, las, los, y, e, da, das, do, dos, van, von, di`.
    - `Name.Empty` crea una instancia sin validar. Documenta que es un objeto nulo que no debe
      persistirse, o sustitúyelo por `Name?` donde se use.
    - `NameFactory` (0 usos fuera del Fw) contradice a `Name`: máximo de 255 frente a 100, y
      algoritmos de kebab/snake case distintos. Si no está registrado en DI ni se usa, elimínalo.
      Si se usa, haz que delegue en las conversiones de `Name`.

13. **`ErrorMessage`** — `Fw.Domain/.../Verticals/Error/ErrorMessage.cs`
    - La expresión `^[\p{L}\p{N}\s\.,;:!¡?¿()]+$` rechaza mensajes legítimos («Can't», «IVA-21%»,
      «a/b»). Sustitúyela por «ningún carácter de control» (`\p{C}` excepto espacios), manteniendo
      los límites de longitud.

14. **`Icon`** — `Fw.Domain/.../Icon/Icon.cs`
    - `FromFile` y `FromStream` fijan 64×64 px, 96 dpi y 32 bpp sea cual sea la imagen: exige esos
      datos como parámetros (o léelos de verdad) en lugar de inventarlos.
    - Es un `record struct` con `byte[]`, así que la igualdad compara referencias: implementa la
      igualdad con `SequenceEqual` (el método privado `MemoryEquals` ya existe y no se usa).

15. **`OrganizationName`** — `Fw.Domain/.../OrganizationName/OrganizationName.cs`
    - La longitud se comprueba **antes** del `Trim()`.
    - La expresión regular excluye caracteres habituales en razones sociales (`/ + ! ( )`), como
      «Yahoo!» o «Hewlett-Packard (HP)». Amplíala.

## Fuera de alcance (no lo hagas en esta tarea)

- Rediseñar `PersonalName` para dos apellidos o conservar mayúsculas internas («McDonald»). Es un
  cambio de modelo del contexto Parties que requiere decisión aparte; solo déjalo anotado.
- Mover tipos entre ensamblados o cambiar namespaces, salvo la consolidación de `Email`.

## Entregable

Resumen final con: defectos corregidos (con el test que lo demuestra), cambios que rompen
compatibilidad y usos adaptados, puntos no aplicados por riesgo sobre datos persistidos (con la
migración propuesta para Oracle y SQL Server) y el resultado de build y tests.
