import { useEffect, useRef } from 'react'
import { basicSetup } from 'codemirror'
import { Compartment, EditorState, type Extension } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { indentWithTab } from '@codemirror/commands'
import { syntaxHighlighting } from '@codemirror/language'
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark'
import { php } from '@codemirror/lang-php'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { html } from '@codemirror/lang-html'
import { css } from '@codemirror/lang-css'
import { markdown } from '@codemirror/lang-markdown'
import { yaml } from '@codemirror/lang-yaml'
import { xml } from '@codemirror/lang-xml'

/** Picks the syntax mode from the file name (.blade.php is PHP mixed with HTML). */
export function languageFor(path: string): Extension {
  const name = path.toLowerCase().split('/').pop() ?? ''
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1) : name
  switch (ext) {
    case 'php':
      return php()
    case 'js':
    case 'mjs':
    case 'cjs':
      return javascript()
    case 'jsx':
      return javascript({ jsx: true })
    case 'ts':
    case 'mts':
      return javascript({ typescript: true })
    case 'tsx':
      return javascript({ jsx: true, typescript: true })
    case 'vue':
    case 'svelte':
    case 'html':
    case 'htm':
      return html()
    case 'json':
    case 'lock':
      return json()
    case 'css':
    case 'scss':
      return css()
    case 'md':
    case 'markdown':
      return markdown()
    case 'yml':
    case 'yaml':
      return yaml()
    case 'xml':
    case 'svg':
    case 'dist':
      return xml()
    default:
      return []
  }
}

// The app's own surfaces, with One Dark's syntax colours on top.
const theme = EditorView.theme(
  {
    '&': { height: '100%', backgroundColor: 'var(--bg-1)', color: 'var(--text)', fontSize: '12.5px' },
    '.cm-scroller': { fontFamily: 'var(--mono)', lineHeight: '1.6' },
    '.cm-content': { caretColor: 'var(--accent)', fontVariantLigatures: 'none' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--accent)' },
    '&.cm-focused': { outline: 'none' },
    '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, ::selection': {
      backgroundColor: 'rgba(255, 106, 61, 0.22) !important',
    },
    '.cm-gutters': { backgroundColor: 'var(--bg-1)', color: 'var(--text-4)', border: 'none', borderRight: '1px solid var(--line)' },
    '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--text-2)' },
    '.cm-activeLine': { backgroundColor: 'rgba(255, 255, 255, 0.03)' },
    '.cm-selectionMatch': { backgroundColor: 'rgba(255, 255, 255, 0.08)' },
    '.cm-matchingBracket, &.cm-focused .cm-matchingBracket': { backgroundColor: 'rgba(255, 255, 255, 0.1)', outline: 'none' },
    '.cm-searchMatch': { backgroundColor: 'rgba(234, 179, 8, 0.25)' },
    '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: 'rgba(234, 179, 8, 0.45)' },
    '.cm-foldPlaceholder': { backgroundColor: 'var(--bg-3)', border: 'none', color: 'var(--text-2)' },
    '.cm-panels': { backgroundColor: 'var(--bg-2)', color: 'var(--text)' },
    '.cm-panels.cm-panels-top': { borderBottom: '1px solid var(--line)' },
    '.cm-panel input, .cm-panel button': { fontFamily: 'inherit' },
    '.cm-tooltip': { backgroundColor: 'var(--bg-3)', border: '1px solid var(--line-2)', color: 'var(--text)' },
    '.cm-tooltip-autocomplete > ul > li[aria-selected]': { backgroundColor: 'var(--accent-soft)', color: 'var(--text)' },
  },
  { dark: true },
)

interface Props {
  /** Identifies the document: a new value replaces the content and history. */
  docKey: string
  value: string
  path: string
  readOnly?: boolean
  onChange: (value: string) => void
  onSave: () => void
}

/** A minimal CodeMirror 6 editor: syntax highlighting, search (⌘F), folding,
 * bracket matching, multiple cursors and ⌘S to save. */
export function CodeEditor(props: Props) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const lang = useRef(new Compartment())
  const ro = useRef(new Compartment())
  // Handlers change every render; the editor reads the latest through refs.
  const onChange = useRef(props.onChange)
  const onSave = useRef(props.onSave)
  onChange.current = props.onChange
  onSave.current = props.onSave

  useEffect(() => {
    const v = new EditorView({ parent: host.current! })
    view.current = v
    return () => {
      v.destroy()
      view.current = null
    }
  }, [])

  // A new document gets a fresh state, so undo never crosses files.
  useEffect(() => {
    const v = view.current
    if (!v) return
    v.setState(
      EditorState.create({
        doc: props.value,
        extensions: [
          basicSetup,
          keymap.of([
            indentWithTab,
            {
              key: 'Mod-s',
              preventDefault: true,
              run: () => {
                onSave.current()
                return true
              },
            },
          ]),
          lang.current.of(languageFor(props.path)),
          ro.current.of(EditorState.readOnly.of(!!props.readOnly)),
          syntaxHighlighting(oneDarkHighlightStyle),
          theme,
          EditorView.updateListener.of((u) => {
            if (u.docChanged) onChange.current(u.state.doc.toString())
          }),
        ],
      }),
    )
    v.focus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.docKey])

  useEffect(() => {
    view.current?.dispatch({ effects: ro.current.reconfigure(EditorState.readOnly.of(!!props.readOnly)) })
  }, [props.readOnly])

  return <div ref={host} className="code-editor" />
}
