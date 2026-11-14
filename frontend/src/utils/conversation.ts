import { i18n } from '@/i18n'

interface ConversationTurn { role: 'user' | 'assistant'; content: string; error?: boolean }

/** Bounded, explicitly labeled conversation data for the stateless grounded Q&A endpoint. */
export function conversationContext(messages: ConversationTurn[]): string {
  const turns: { role: string; content: string }[] = []
  for (const message of messages.filter(item => !item.error).slice(-12).reverse()) {
    const next = { role: message.role, content: message.content.slice(0, 4000) }
    if (JSON.stringify([next, ...turns]).length > 15500) break
    turns.unshift(next)
  }
  return turns.length ? `${i18n.global.t('copilot.historyPreamble')}${JSON.stringify(turns)}\n\n` : ''
}
