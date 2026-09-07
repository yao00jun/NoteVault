export interface CopilotRequest {
  id: string
  prompt: string
  context: string
  source?: string
  workspacePath?: string
  onApply?: (content: string) => void
}

/** Route domain context into the existing global Copilot without coupling views. */
export function requestCopilot(request: Omit<CopilotRequest, 'id'>): string {
  const id = crypto.randomUUID()
  window.dispatchEvent(new CustomEvent<CopilotRequest>('notevault:copilot-request', { detail: { ...request, id } }))
  return id
}
