import { Call } from '@wailsio/runtime'

export type CollectionKind = 'project' | 'book' | 'topic'
export type SourceImportType = 'empty' | 'folder' | 'adopt' | 'files' | 'url'

export interface SourceImportOpenRequest {
  kind?: CollectionKind
  sourceType?: SourceImportType
  source?: string
  name?: string
  targetFolder?: string
  instruction?: string
  autoStart?: boolean
}

export interface SourceImportFile {
  name: string
  contentBase64: string
}

export interface SourceImportRequest {
  sourceType: SourceImportType
  source: string
  files: SourceImportFile[]
  kind: CollectionKind
  name: string
  targetFolder: string
  conflictStrategy: 'skip' | 'update' | 'copy'
  enrich: boolean
  instruction: string
  ai: { apiKey: string; baseURL: string; model: string }
}

export interface SourceImportPreview {
  name: string
  kind: CollectionKind
  targetFolder: string
  metadataPath: string
  fileCount: number
  files: string[]
  warnings: string[]
  existing: boolean
}

export interface SourceImportResult {
  taskId: string
  targetFolder: string
  metadataPath: string
  imported: number
  skipped: number
  updated: number
  conflicts: string[]
  warnings: string[]
  files: string[]
  cancelled: boolean
}

const service = 'github.com/notevault/notevault/internal/service.ImportService'

export const SourceImportService = {
  PreviewSourceImport(workspacePath: string, request: SourceImportRequest): Promise<SourceImportPreview> {
    return Call.ByName(`${service}.PreviewSourceImport`, workspacePath, request)
  },
  StartSourceImport(workspacePath: string, request: SourceImportRequest): Promise<string> {
    return Call.ByName(`${service}.StartSourceImport`, workspacePath, request)
  },
  GetSourceImportResult(workspacePath: string, taskId: string): Promise<SourceImportResult> {
    return Call.ByName(`${service}.GetSourceImportResult`, workspacePath, taskId)
  },
}
