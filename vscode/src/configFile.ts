/**
 * Adds a pattern to the "exclude" list of a config file's JSON text.
 * @param text Current contents of the config file.
 * @param pattern Exclude pattern, e.g. `node_modules` or `packages/app/node_modules`.
 * @returns The new contents, or undefined when the pattern is already excluded.
 * @throws When the text is not a JSON object.
 */
export function withExclude(text: string, pattern: string): string | undefined {
  const config: unknown = JSON.parse(text);
  if (!config || typeof config !== 'object' || Array.isArray(config)) {
    throw new Error('the config is not a JSON object');
  }
  const record = config as Record<string, unknown>;
  const exclude = Array.isArray(record.exclude) ? record.exclude : [];
  if (exclude.includes(pattern)) {
    return undefined;
  }
  record.exclude = [...exclude, pattern];
  return JSON.stringify(record, null, 2) + '\n';
}
