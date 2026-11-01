// A small fuzzy matcher for file paths, in the spirit of fzf and VS Code's
// quick open: the query's characters must appear in order, and the best
// alignment wins. Matches in the file name, at the start of a path segment or
// word (after / _ - . or a camelCase hump) and in contiguous runs score more;
// gaps cost a little. So "cdedt" finds components/CodeEditor.tsx.

export interface FuzzyMatch {
  score: number
  /** Indexes of the matched characters in the path, ascending. */
  positions: number[]
}

const MATCH = 16
const BONUS_SEGMENT = 10 // first character, or right after '/'
const BONUS_WORD = 8 // after _ - . or space
const BONUS_CAMEL = 7 // lower-to-upper or letter-to-digit hump
const BONUS_CONSECUTIVE = 6
const BONUS_BASENAME = 6
const BONUS_CASE = 1
const GAP_START = 3
const GAP_EXTEND = 1

// Scratch space reused between calls: a score and a back-pointer per
// (query char, path char) cell.
let scores = new Float64Array(0)
let back = new Int32Array(0)

function bonusAt(path: string, j: number): number {
  if (j === 0) return BONUS_SEGMENT
  const prev = path[j - 1]
  const cur = path[j]
  if (prev === '/' || prev === '\\') return BONUS_SEGMENT
  if (prev === '_' || prev === '-' || prev === '.' || prev === ' ') return BONUS_WORD
  const prevLower = prev >= 'a' && prev <= 'z'
  const curUpper = cur >= 'A' && cur <= 'Z'
  const curDigit = cur >= '0' && cur <= '9'
  const prevDigit = prev >= '0' && prev <= '9'
  if (prevLower && curUpper) return BONUS_CAMEL
  if (curDigit && !prevDigit) return BONUS_CAMEL
  return 0
}

/**
 * Scores path against query. Returns null when the query's characters (spaces
 * ignored, case-insensitive) do not all appear in order in the path.
 */
export function fuzzyMatch(query: string, path: string): FuzzyMatch | null {
  const q = query.replace(/\s+/g, '')
  if (!q) return { score: 0, positions: [] }
  const ql = q.toLowerCase()
  const pl = path.toLowerCase()
  const m = ql.length
  const n = pl.length
  if (m > n) return null

  // Quick reject: is the query a subsequence at all? Also bounds the search:
  // query char i cannot match before first[i] nor after last[i].
  const first = new Int32Array(m)
  for (let i = 0, j = 0; i < m; i++, j++) {
    while (j < n && pl[j] !== ql[i]) j++
    if (j === n) return null
    first[i] = j
  }
  const last = new Int32Array(m)
  for (let i = m - 1, j = n - 1; i >= 0; i--, j--) {
    while (pl[j] !== ql[i]) j--
    last[i] = j
  }

  const base = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')) + 1
  if (scores.length < m * n) {
    scores = new Float64Array(m * n * 2)
    back = new Int32Array(m * n * 2)
  }
  const NEG = -Infinity

  for (let i = 0; i < m; i++) {
    const row = i * n
    const prevRow = row - n
    // Best predecessor reached through a gap: max over k <= j-2 of
    // scores[prev][k] - GAP_START - GAP_EXTEND * (j - k - 2).
    let gapBest = NEG
    let gapK = -1
    for (let j = 0; j < n; j++) {
      if (i > 0 && j >= 2) {
        const cand = scores[prevRow + j - 2] - GAP_START
        gapBest -= GAP_EXTEND
        if (cand >= gapBest) {
          gapBest = cand
          gapK = j - 2
        }
      }
      if (j < first[i] || j > last[i] || pl[j] !== ql[i]) {
        scores[row + j] = NEG
        continue
      }
      let s = MATCH + bonusAt(path, j)
      if (j >= base) s += BONUS_BASENAME
      if (path[j] === q[i]) s += BONUS_CASE
      if (i === 0) {
        scores[row + j] = s
        back[row + j] = -1
        continue
      }
      const consec = j >= 1 ? scores[prevRow + j - 1] + BONUS_CONSECUTIVE : NEG
      if (consec === NEG && gapBest === NEG) {
        scores[row + j] = NEG
      } else if (consec >= gapBest) {
        scores[row + j] = consec + s
        back[row + j] = j - 1
      } else {
        scores[row + j] = gapBest + s
        back[row + j] = gapK
      }
    }
  }

  const lastRow = (m - 1) * n
  let best = NEG
  let bestJ = -1
  for (let j = first[m - 1]; j <= last[m - 1]; j++) {
    if (scores[lastRow + j] > best) {
      best = scores[lastRow + j]
      bestJ = j
    }
  }
  if (bestJ < 0) return null

  const positions = new Array<number>(m)
  for (let i = m - 1, j = bestJ; i >= 0; i--) {
    positions[i] = j
    j = back[i * n + j]
  }
  // Among equal alignments, shorter paths win.
  return { score: best - n * 0.01, positions }
}

export interface RankedPath extends FuzzyMatch {
  path: string
}

/** The best `limit` paths for query, best first. */
export function rankPaths(query: string, paths: string[], limit = 50): RankedPath[] {
  if (!query.trim()) return paths.slice(0, limit).map((path) => ({ path, score: 0, positions: [] }))
  const out: RankedPath[] = []
  for (const path of paths) {
    const r = fuzzyMatch(query, path)
    if (r) out.push({ path, ...r })
  }
  out.sort((a, b) => b.score - a.score || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
  return out.slice(0, limit)
}
