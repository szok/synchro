/** Splits a folder name into words on `-`, `_`, `.`, spaces and camelCase humps. */
export function words(name: string): string[] {
  return name
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1 $2')
    .split(/[\s._-]+/)
    .filter((w) => w !== '');
}

/**
 * Short status bar labels for folder names: the initials of their words
 * ("ring-websites-cdh-web" → "rwcw"). Single-word names stay whole. Labels
 * that would collide take more letters of the last word, then the full name.
 */
export function shortLabels(names: string[]): string[] {
  const split = names.map(words);
  const labels = split.map((w, i) => (w.length < 2 ? names[i] : label(w, 1)));
  for (let extra = 2; ; extra++) {
    const colliding = labels.map((l, i) => labels.some((other, j) => j !== i && other === l));
    if (!colliding.some(Boolean)) {
      return labels;
    }
    let changed = false;
    labels.forEach((l, i) => {
      if (!colliding[i] || split[i].length < 2) {
        return;
      }
      const last = split[i][split[i].length - 1];
      const next = extra <= last.length ? label(split[i], extra) : names[i];
      changed ||= next !== l;
      labels[i] = next;
    });
    if (!changed) {
      return labels;
    }
  }
}

/**
 * Builds a label from the initials of all but the last word plus the first letters of the last word.
 * @param w Words of the folder name.
 * @param lastLetters How many letters of the last word to keep.
 */
function label(w: string[], lastLetters: number): string {
  const initials = w.slice(0, -1).map((word) => word[0]);
  return [...initials, w[w.length - 1].slice(0, lastLetters)].join('').toLowerCase();
}
