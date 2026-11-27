// El foco sobrevive a un cambio de ficha: la ficha se vuelve a montar al guardar, cancelar o pasar al
// registro anterior/siguiente, y el foco debe seguir dentro de ella (si no, los atajos del ámbito `detail`
// dejan de valer). Quien provoca el cambio lo pide aquí y la cabecera de la ficha nueva lo recoge.
let pending = false;

export function requestSheetFocus(): void {
  pending = true;
}

/** `true` una sola vez por petición. */
export function takeSheetFocus(): boolean {
  const value = pending;
  pending = false;
  return value;
}
