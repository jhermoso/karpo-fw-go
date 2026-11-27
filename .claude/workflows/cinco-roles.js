export const meta = {
  name: 'cinco-roles',
  description: 'Analista, planificador y constructor, cada uno en bucle con su juez, más un asesor (docs/FORMA-DE-TRABAJO.md)',
  whenToUse: 'Tareas medianas o grandes de backend o front en Karpo Go. Args: {tarea, objetivo, tipo, carpeta, rama, desde?, previo?, arbitrada?}',
  phases: [
    { title: 'Análisis', detail: 'analista ↔ juez-analisis, hasta 3 vueltas' },
    { title: 'Plan', detail: 'planificador ↔ juez-plan, hasta 3 vueltas' },
    { title: 'Construcción', detail: 'constructor ↔ juez-construccion (+ juez-ux en front), hasta 3 vueltas' },
    { title: 'Memoria', detail: 'escribe docs/trabajo/<tarea>/' },
  ],
}

// args: {tarea, objetivo, tipo: implementacion|testing|front|devops|documentacion, carpeta: worktree
// ya creado (ruta absoluta), rama}. Para relanzar tras el arbitraje (§3 y §7 de
// docs/FORMA-DE-TRABAJO.md): desde: 'plan'|'construccion', previo: la entrega aprobada del rol
// anterior, arbitrada: la entrega corregida por el orquestador, que el juez juzga primero.
const A = args || {}
if (!A.tarea || !A.objetivo || !A.carpeta) {
  throw new Error('faltan args: tarea, objetivo y carpeta son obligatorios')
}
const TIPO = A.tipo || 'implementacion'
const VUELTAS = 3
const ROLES = ['analista', 'planificador', 'constructor']
const FASES = { analista: 'Análisis', planificador: 'Plan', constructor: 'Construcción' }
const JUECES = {
  analista: ['juez-analisis'],
  planificador: ['juez-plan'],
  constructor: TIPO === 'front' ? ['juez-construccion', 'juez-ux'] : ['juez-construccion'],
}
const INSUMO = {
  analista: () => 'Haz el análisis.',
  planificador: prev => `Escribe el plan a partir de este análisis aprobado:\n\n${prev}`,
  constructor: prev => `Ejecuta este plan aprobado:\n\n${prev}`,
}
const CONTEXTO = `Tarea «${A.tarea}» (tipo ${TIPO}). Objetivo: ${A.objetivo}
Trabaja en la carpeta ${A.carpeta}${A.rama ? ` (rama ${A.rama})` : ''}: ahí están el código y docs/FORMA-DE-TRABAJO.md, que rige este trabajo.`

const ENTREGA = {
  type: 'object',
  properties: {
    entrega: { type: 'string', description: 'La entrega completa, en Markdown, en español' },
    consultas: { type: 'array', items: { type: 'string' }, description: 'Preguntas concretas para el asesor; vacío si no hay' },
  },
  required: ['entrega', 'consultas'],
}
const VEREDICTO = {
  type: 'object',
  properties: {
    aprobado: { type: 'boolean' },
    hallazgos: { type: 'array', items: { type: 'string' }, description: 'Qué criterio incumple y qué hace falta para aprobar' },
    desviaciones: { type: 'array', items: { type: 'string' }, description: 'Desviaciones de los patrones DDD justificadas, para Javier' },
    consultas: { type: 'array', items: { type: 'string' } },
    origen: { type: 'string', enum: ['', 'analista', 'planificador'], description: 'Rol anterior donde nace el fallo, si no es el juzgado; vacío si no' },
  },
  required: ['aprobado', 'hallazgos', 'desviaciones', 'consultas', 'origen'],
}

// Cada agente lee la definición de su rol en la carpeta de la tarea.
const rolDe = r => `Actúas como el rol «${r}»: lee primero ${A.carpeta}/.claude/agents/${r}.md y sigue sus instrucciones.\n\n`
const lista = xs => xs.map(x => `- ${x}`).join('\n')

const registro = { consultas: [], veredictos: [], desviaciones: [] }

async function asesorar(quien, texto, fase) {
  const r = await agent(`${rolDe('asesor')}${CONTEXTO}\n\nConsulta de ${quien}:\n${texto}`,
    { model: 'fable', effort: 'medium', phase: fase, label: `asesor ← ${quien}` })
  registro.consultas.push({ quien, preguntas: [texto], respuesta: r || '' })
  return r || ''
}

async function juzgar(juez, entrega, fase, vuelta) {
  let v = await agent(`${rolDe(juez)}${CONTEXTO}\n\nJuzga esta entrega (vuelta ${vuelta}):\n\n${entrega}`,
    { schema: VEREDICTO, effort: 'high', phase: fase, label: `${juez} v${vuelta}` })
  if (v && v.consultas.length) {
    const resp = await asesorar(juez, lista(v.consultas), fase)
    v = await agent(`${rolDe(juez)}${CONTEXTO}\n\nRespuestas del asesor a tus consultas:\n${resp}\n\nJuzga esta entrega (vuelta ${vuelta}):\n\n${entrega}`,
      { schema: VEREDICTO, effort: 'high', phase: fase, label: `${juez} v${vuelta}b` })
  }
  return v || { aprobado: false, hallazgos: ['el juez no respondió'], desviaciones: [], consultas: [], origen: '' }
}

// Una vuelta de jueces sobre una entrega. Devuelve {ok, hallazgos, origen}.
async function vueltaDeJueces(rol, entrega, vuelta) {
  const fase = FASES[rol]
  const jueces = JUECES[rol]
  const vs = await parallel(jueces.map(j => () => juzgar(j, entrega, fase, vuelta)))
  let ok = true, hallazgos = [], origen = ''
  vs.forEach((v, i) => {
    registro.veredictos.push({ fase, vuelta, juez: jueces[i], ...v })
    registro.desviaciones.push(...v.desviaciones)
    if (!v.aprobado) {
      ok = false
      hallazgos.push(...v.hallazgos.map(h => `[${jueces[i]}] ${h}`))
      if (v.origen && ROLES.indexOf(v.origen) < ROLES.indexOf(rol)) origen = v.origen
    }
  })
  log(`${fase} vuelta ${vuelta}: ${ok ? 'aprobada' : `${hallazgos.length} hallazgos${origen ? `, nace en ${origen}` : ''}`}`)
  return { ok, hallazgos, origen }
}

// El bucle de un rol con su juez (§3). Devuelve {aprobado, entrega, hallazgos, origen?}.
async function bucle(rol, prev, extra) {
  const fase = FASES[rol]
  let entrega = '', hallazgos = [], directrices = extra || ''
  for (let vuelta = 1; vuelta <= VUELTAS; vuelta++) {
    const encargo = `${rolDe(rol)}${CONTEXTO}\n\n${INSUMO[rol](prev)}` +
      (directrices ? `\n\nDirectrices del asesor:\n${directrices}` : '') +
      (hallazgos.length ? `\n\nTu entrega anterior:\n${entrega}\n\nHallazgos de los jueces que debes resolver:\n${lista(hallazgos)}` : '')
    let r = await agent(encargo, { schema: ENTREGA, phase: fase, label: `${rol} v${vuelta}` })
    if (!r) return { aprobado: false, entrega, hallazgos: [`${rol} no respondió`] }
    if (r.consultas.length) {
      const resp = await asesorar(rol, lista(r.consultas), fase)
      r = (await agent(`${encargo}\n\nRespuestas del asesor:\n${resp}\n\nRehaz tu entrega con ellas.`,
        { schema: ENTREGA, phase: fase, label: `${rol} v${vuelta}b` })) || r
    }
    entrega = r.entrega
    const j = await vueltaDeJueces(rol, entrega, vuelta)
    if (j.ok) return { aprobado: true, entrega }
    hallazgos = j.hallazgos
    if (j.origen) return { aprobado: false, entrega, hallazgos, origen: j.origen }
    // Desde el segundo suspenso, el asesor da directrices antes de rehacer.
    if (vuelta >= 2 && vuelta < VUELTAS) {
      directrices = await asesorar(`orquestador (por ${rol})`,
        `El juez ha suspendido ${vuelta} veces esta entrega del ${rol}. Da directrices concretas para que apruebe a la siguiente.\n\nEntrega:\n${entrega}\n\nHallazgos:\n${lista(hallazgos)}`, fase)
    }
  }
  return { aprobado: false, entrega, hallazgos, motivo: `${VUELTAS} vueltas sin aprobar` }
}

// Mapas sin prototipo: con {} , entregas['constructor'] sería la función Object.
const resultado = { tarea: A.tarea, tipo: TIPO, entregas: Object.create(null) }
let devoluciones = 0
let i = A.desde ? ROLES.indexOf(A.desde === 'plan' ? 'planificador' : A.desde === 'construccion' ? 'constructor' : A.desde) : 0
if (i < 0) throw new Error(`desde no válido: ${A.desde}`)
let prev = A.previo || ''
let extra = Object.create(null), parada = null

// Relanzamiento tras el arbitraje: el juez juzga primero la entrega corregida, una vuelta más.
if (A.arbitrada) {
  const rol = ROLES[i]
  phase(FASES[rol])
  const j = await vueltaDeJueces(rol, A.arbitrada, 'arbitraje')
  if (!j.ok) {
    parada = { rol, motivo: 'no aprobada tras el arbitraje: decide Javier', hallazgos: j.hallazgos, entrega: A.arbitrada }
  } else {
    resultado.entregas[rol] = A.arbitrada
    prev = A.arbitrada
    i++
  }
}

while (!parada && i < ROLES.length) {
  const rol = ROLES[i]
  phase(FASES[rol])
  const r = await bucle(rol, prev, extra[rol])
  if (r.aprobado) {
    resultado.entregas[rol] = r.entrega
    prev = r.entrega
    i++
    continue
  }
  if (r.origen && devoluciones < 1) {
    // Devolver a un rol anterior, con directrices del asesor (§3, punto 5).
    devoluciones++
    const directrices = await asesorar(`orquestador (devolución de ${rol} a ${r.origen})`,
      `El juez del ${rol} dice que el fallo nace en el ${r.origen}. Da directrices para que el ${r.origen} lo corrija.\n\nHallazgos:\n${lista(r.hallazgos)}\n\nEntrega del ${rol}:\n${r.entrega}`, FASES[rol])
    extra[r.origen] = `${directrices}\n\nHallazgos que motivan la devolución:\n${lista(r.hallazgos)}`
    i = ROLES.indexOf(r.origen)
    prev = i === 0 ? '' : resultado.entregas[ROLES[i - 1]]
    log(`Devuelto a ${r.origen}`)
    continue
  }
  parada = { rol, motivo: r.origen ? 'segunda devolución: decide Javier' : (r.motivo || 'sin aprobar'), hallazgos: r.hallazgos, entrega: r.entrega }
}

phase('Memoria')
const nombres = { analista: '01-analisis.md', planificador: '02-plan.md', constructor: '03-construccion.md' }
const secciones = [
  ...ROLES.filter(r => resultado.entregas[r]).map(r => [nombres[r], resultado.entregas[r]]),
  ...(parada ? [[`${nombres[parada.rol].replace('.md', '')}-parado.md`, `${parada.motivo}\n\n${lista(parada.hallazgos)}\n\n---\n\n${parada.entrega}`]] : []),
  ['veredictos.md', registro.veredictos.map(v => `## ${v.fase}, vuelta ${v.vuelta}: ${v.juez}: ${v.aprobado ? 'aprobado' : 'no aprobado'}${v.origen ? ` (nace en ${v.origen})` : ''}\n\n${lista(v.hallazgos)}${v.desviaciones.length ? `\n\nDesviaciones:\n${lista(v.desviaciones)}` : ''}`).join('\n\n')],
  ['consultas.md', registro.consultas.map(c => `## ${c.quien}\n\n${c.preguntas.join('\n')}\n\n${c.respuesta}`).join('\n\n')],
].filter(([, t]) => t)
await agent(`Escribe estos ficheros, tal cual, en ${A.carpeta}/docs/trabajo/${A.tarea}/ (crea la carpeta; si un fichero ya existe, añade el texto nuevo al final). No cambies el contenido ni hagas commit.\n\n` +
  secciones.map(([f, t]) => `=== ${f} ===\n${t}`).join('\n\n'), { phase: 'Memoria', model: 'haiku', effort: 'low', label: 'memoria' })

resultado.desviacionesParaJavier = registro.desviaciones
resultado.parada = parada
return resultado
