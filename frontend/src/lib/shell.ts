// Quoting for text Larvyn hands to a shell: paths pasted into a terminal and
// command lines copied to the clipboard.

/** A path as the shell reads it, or null when it can't be quoted safely.
 * Nothing in a file name may run, or expand, when the line is submitted:
 * - off Windows: single quotes, which POSIX shells and fish take literally;
 * - on Windows, the terminal may be PowerShell, cmd.exe (delayed expansion
 *   on or off), Git Bash, or one started inside another, and Larvyn can't
 *   tell which reads the line. So one rule safe in all of them: double
 *   quotes, refusing what any of them expands or ends a string with there:
 *   $ and ` (PowerShell, bash), % and ! (cmd), and every double quote,
 *   straight or curly (PowerShell reads “ ” „ as quotes too). */
export function quotePath(p: string, windows: boolean): string | null {
  // No ~: unquoted, a leading ~ (~/x, ~root) expands to a home folder.
  if (!windows) return /^[A-Za-z0-9/._\-+=,:@%]+$/.test(p) ? p : `'${p.replace(/'/g, `'\\''`)}'`
  // A trailing backslash would escape the closing quote in Git Bash and
  // leave the rest of the line unquoted; a folder's path means the same
  // without it (a drive root keeps a dot: C:\.).
  p = p.replace(/\\+$/, '')
  if (/^[A-Za-z]:$/.test(p)) p += '\\.'
  if (/^[A-Za-z0-9/._\-+:@]+$/.test(p)) return p
  if (/["“”„$`%!]/.test(p)) return null
  return `"${p}"`
}

/** A command line to copy, with every argument quoted as quotePath does, or
 * null when one can't be quoted safely (only on Windows: $ ` % ! or a double
 * quote in an argument). An argument with a control character (a newline
 * would end the command) is refused on every platform. */
export function shellLine(argv: string[], windows: boolean): string | null {
  const out: string[] = []
  for (const a of argv) {
    // quotePath drops a trailing backslash, which is right for a folder but
    // would change any other argument.
    if (windows && a.endsWith('\\')) return null
    const q = /[\u0000-\u001f\u007f-\u009f]/.test(a) ? null : a === '' ? (windows ? '""' : "''") : quotePath(a, windows)
    if (q === null) return null
    out.push(q)
  }
  return out.join(' ')
}
