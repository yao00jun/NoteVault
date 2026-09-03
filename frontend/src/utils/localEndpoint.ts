/**
 * 本机端点判定（与后端 requireCredential 的"本机免 Key"口径对齐）。
 *
 * 后端用真实 DNS 解析判断回环；前端只需要覆盖 URL 里显式写的本机地址
 * （localhost / 127.x / [::1] / *.localhost），这是用户"本机 Ollama /
 * LM Studio"场景的实际写法。判断不了时按云端处理（要求 Key），宁可多问一次。
 */
export function isLocalBaseURL(baseURL: string | null | undefined): boolean {
  if (!baseURL) return false
  try {
    const u = new URL(baseURL)
    const host = u.hostname.toLowerCase()
    return (
      host === 'localhost' ||
      host.endsWith('.localhost') ||
      host === '::1' ||
      host === '[::1]' ||
      /^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(host) ||
      host === '0.0.0.0'
    )
  } catch {
    return false
  }
}
