import { reactive } from 'vue'

export const editorSession = reactive({
  workspacePath: '', path: '', state: 'idle' as 'idle' | 'saved' | 'dirty' | 'saving' | 'error' | 'conflict',
  error: '', line: 1, column: 1, words: 0, dirtyCount: 0, draftPath: '',
})

let flush: (() => Promise<boolean>) | undefined
export function registerEditorFlush(handler: () => Promise<boolean>) {
  flush = handler
  return () => { if (flush === handler) flush = undefined }
}
export async function flushOpenEditor(): Promise<boolean> { return flush ? flush() : true }
export function updateEditorCursor(line: number, column: number) {
  editorSession.line = line
  editorSession.column = column
}
