/**
 * useAiEndpointSettings - 设置页 AI 三区块（AI 总结 / 语义检索 / 重排序）
 * 的端点预设与连通性自检逻辑。
 *
 * 从 SettingsView 抽出（模板与样式原地不动，纯逻辑搬运）：
 * 三块共享同一组预设与自检状态，全部直接读写全局 settingsStore。
 */
import { LLMConfigService, LLMEndpointPreset, LLMProbeResult, RerankProbeResult, EmbeddingProbeResult } from '@/api'
import { ref } from 'vue'
import { i18n } from '@/i18n'
import { useSettingsStore } from '@/stores/settings'
import type { RerankProvider } from '@/api'

export function useAiEndpointSettings() {
  const t = i18n.global.t
  const settingsStore = useSettingsStore()

  // --- P1-6 端点预设与连通性自检 ---
  // 本机端点（Ollama / LM Studio）不需要 API Key。后端 requireCredential 会放行，
  // 这里只负责让用户不必手记地址和端口。
  const presets = ref<LLMEndpointPreset[]>([])
  const probing = ref(false)
  const probeResult = ref<LLMProbeResult | null>(null)

  // --- P1-3b rerank 自检（与 AI 端点自检同源）---
  const rerankProbing = ref(false)
  const rerankProbeResult = ref<RerankProbeResult | null>(null)

  // --- P1-3 embedding 自检（语义检索端点能否返回向量）---
  const embeddingProbing = ref(false)
  const embeddingProbeResult = ref<EmbeddingProbeResult | null>(null)

  // P1-3b：rerank 预设（与 AI 预设同源思路，但 rerank 走独立配置段）。
  // 硅基流动接口与 Cohere 同格式（/v1/rerank + Bearer），故复用 provider='cohere' 路径。
  // 本地两档单独给 provider：llama.cpp 与 TEI 的请求 schema 不同（documents vs texts），
  // 混用会让自检通过、真实检索失败。
  const rerankPresets: Array<{
    id: string
    label: string
    provider: 'cohere' | 'llamacpp' | 'tei'
    baseURL: string
    model: string
    hint: string
  }> = [
    {
      id: 'llamacpp',
      label: 'llama-server',
      provider: 'llamacpp',
      baseURL: 'http://127.0.0.1:8080/v1',
      model: 'bge-reranker-v2-m3',
      hint: t('settings.rerank.presetLlamaCppHint'),
    },
    {
      id: 'tei',
      label: 'TEI',
      provider: 'tei',
      baseURL: 'http://127.0.0.1:8081',
      model: 'BAAI/bge-reranker-v2-m3',
      hint: t('settings.rerank.presetTEIHint'),
    },
    {
      id: 'siliconflow',
      label: t('settings.embedding.providerSiliconflow'),
      provider: 'cohere',
      baseURL: 'https://api.siliconflow.cn/v1',
      model: 'BAAI/bge-reranker-v2-m3',
      hint: t('settings.rerank.presetSiliconflowHint'),
    },
    {
      id: 'cohere',
      label: 'Cohere',
      provider: 'cohere',
      baseURL: 'https://api.cohere.ai/v1',
      model: 'rerank-multilingual-v3.0',
      hint: t('settings.rerank.presetCohereHint'),
    },
  ]
  function applyRerankPreset(id: string) {
    const p = rerankPresets.find((x) => x.id === id)
    if (!p) return
    settingsStore.settings.rerank.provider = p.provider
    settingsStore.settings.rerank.baseURL = p.baseURL
    settingsStore.settings.rerank.model = p.model
    // 本机端点不校验凭据；清掉旧云端 Key，避免把它发给本机服务
    if (p.provider !== 'cohere') settingsStore.settings.rerank.apiKey = ''
    rerankProbeResult.value = null
  }

  // P1-3：embedding 云端预设（与 rerank 同源思路，但 embedding 走独立配置段，无 provider 开关）。
  // 硅基流动 /v1/embeddings 与 OpenAI 同格式，直接复用 embedding 段。
  const embeddingPresets = [
    {
      id: 'ollama',
      label: 'Ollama',
      baseURL: 'http://localhost:11434/v1',
      model: 'bge-m3',
      hint: t('settings.embedding.presetOllamaHint'),
    },
    {
      id: 'siliconflow',
      label: t('settings.embedding.providerSiliconflow'),
      baseURL: 'https://api.siliconflow.cn/v1',
      model: 'BAAI/bge-m3',
      hint: t('settings.embedding.presetSiliconflowHint'),
    },
    {
      id: 'cohere',
      label: 'Cohere',
      baseURL: 'https://api.cohere.ai/compatibility/v1',
      model: 'embed-multilingual-v3.0',
      hint: t('settings.embedding.presetCohereHint'),
    },
  ]
  function applyEmbeddingPreset(id: string) {
    const p = embeddingPresets.find((x) => x.id === id)
    if (!p) return
    // 云端端点需要 Key，故保留用户已填的 apiKey，不清除
    settingsStore.settings.embedding.provider = p.id as 'ollama' | 'siliconflow' | 'cohere'
    settingsStore.settings.embedding.baseURL = p.baseURL
    settingsStore.settings.embedding.model = p.model
    embeddingProbeResult.value = null
  }

  void (async () => {
    try {
      presets.value = (await LLMConfigService.Presets() ?? []) as LLMEndpointPreset[]
    } catch {
      // 预设拉取失败不影响手填地址，静默降级
      presets.value = []
    }
  })()

  function applyPreset(id: string) {
    const p = presets.value.find((x) => x.id === id)
    if (!p) return
    // 当前预设均为 OpenAI 兼容端点，协议统一回 openai-chat
    settingsStore.settings.ai.protocol = 'openai-chat'
    settingsStore.settings.ai.baseURL = p.baseURL
    settingsStore.settings.ai.model = p.model
    if (!p.requiresKey) {
      // 切到本机端点时清掉旧的云端 Key，避免误发到本机服务
      settingsStore.settings.ai.apiKey = ''
    }
    probeResult.value = null
  }

  async function probeEndpoint() {
    probing.value = true
    probeResult.value = null
    try {
      probeResult.value = await LLMConfigService.Probe(
        settingsStore.settings.ai.apiKey ?? '',
        settingsStore.settings.ai.baseURL ?? '',
        settingsStore.settings.ai.protocol ?? 'openai-chat',
        settingsStore.settings.ai.model ?? '',
      ) as LLMProbeResult
    } catch (e) {
      probeResult.value = {
        ok: false,
        endpoint: '',
        isLocal: false,
        model: '',
        models: [],
        latencyMs: 0,
        message: e instanceof Error ? e.message : String(e),
      }
    } finally {
      probing.value = false
    }
  }

  // 重排端点自检：复用后端 LLMConfigService.ProbeRerank，
  // 探测的地址与实际重排请求完全一致（见 rerankEndpointURL），
  // 因此能明确暴露「Ollama 无 /api/rerank」这类原本会被静默降级的情况。
  async function probeRerankEndpoint() {
    rerankProbing.value = true
    rerankProbeResult.value = null
    const r = settingsStore.settings.rerank
    try {
      rerankProbeResult.value = (await LLMConfigService.ProbeRerank({
        provider: r.provider as unknown as RerankProvider,
        baseURL: r.baseURL ?? '',
        model: r.model ?? '',
        apiKey: r.apiKey ?? '',
      })) as RerankProbeResult
    } catch (e) {
      rerankProbeResult.value = {
        ok: false,
        endpoint: '',
        isLocal: false,
        latencyMs: 0,
        message: e instanceof Error ? e.message : String(e),
      }
    } finally {
      rerankProbing.value = false
    }
  }

  // 语义检索端点自检：复用后端 LLMConfigService.ProbeEmbedding，
  // 探测的地址与实际嵌入请求完全一致（normalizeBaseURL + /embeddings），
  // 明确暴露「端点 404 / Key 失效 / 模型不支持 embedding」等原本要等到建向量索引才暴露的问题。
  async function probeEmbeddingEndpoint() {
    embeddingProbing.value = true
    embeddingProbeResult.value = null
    const e = settingsStore.settings.embedding
    try {
      embeddingProbeResult.value = (await LLMConfigService.ProbeEmbedding(
        e.apiKey ?? '',
        e.baseURL ?? '',
        e.model ?? '',
      )) as EmbeddingProbeResult
    } catch (err) {
      embeddingProbeResult.value = {
        ok: false,
        endpoint: '',
        isLocal: false,
        latencyMs: 0,
        message: err instanceof Error ? err.message : String(err),
      }
    } finally {
      embeddingProbing.value = false
    }
  }

  // 自检返回了模型列表时，一键把当前模型换成列表里的（本机模型名通常带 tag，手打易错）
  function useModel(name: string) {
    settingsStore.settings.ai.model = name
  }

  return {
    presets,
    probing,
    probeResult,
    rerankProbing,
    rerankProbeResult,
    embeddingProbing,
    embeddingProbeResult,
    rerankPresets,
    embeddingPresets,
    applyRerankPreset,
    applyEmbeddingPreset,
    applyPreset,
    probeEndpoint,
    probeRerankEndpoint,
    probeEmbeddingEndpoint,
    useModel,
  }
}
