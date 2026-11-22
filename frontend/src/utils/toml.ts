export function escapeTomlBasicString(value: string): string {
  return value
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    // TOML basic strings forbid unescaped control characters.
    // eslint-disable-next-line no-control-regex
    .replace(/[\x00-\x1f\x7f]/g, (character) => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`)
}
