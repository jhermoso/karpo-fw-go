// Recorridos «vistos» por pantalla, en localStorage con try/catch (PLAN §4.8).
import { readStored, writeStored } from '../theme/storage';

export const SEEN_STORAGE_KEY = 'karpo.help.seen';

function readSeen(): Record<string, true> {
  const raw = readStored(SEEN_STORAGE_KEY);
  if (raw === null) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    return typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)
      ? (parsed as Record<string, true>)
      : {};
  } catch {
    return {};
  }
}

export function isTourSeen(screen: string): boolean {
  return readSeen()[screen] === true;
}

export function markTourSeen(screen: string): void {
  const seen = readSeen();
  if (seen[screen] === true) return;
  seen[screen] = true;
  writeStored(SEEN_STORAGE_KEY, JSON.stringify(seen));
}

/** Olvida los recorridos vistos de una pantalla o de todas. */
export function resetTourSeen(screen?: string): void {
  if (screen === undefined) {
    writeStored(SEEN_STORAGE_KEY, null);
    return;
  }
  const seen = readSeen();
  delete seen[screen];
  writeStored(SEEN_STORAGE_KEY, JSON.stringify(seen));
}
