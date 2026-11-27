// `prepublishOnly`: este paquete no se publica todavía. Falla siempre y explica por qué.
console.error(
  [
    '@karpo/react NO se publica.',
    'El encargo prepara el empaquetado y la versión, pero la publicación en npm es una decisión de',
    'Javier (licencia Q-3, nombre del paquete y tema por defecto Q-2 siguen abiertos).',
    'Para comprobar el contenido del paquete: npm run pack:check',
  ].join('\n'),
);
process.exit(1);
