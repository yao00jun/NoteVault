import { Call } from '@wailsio/runtime'

export interface DistillRequest {
  sourceFile: string
  targetMode: 'book' | 'interview'
  targetFolder: string
  targetTitle: string
  question: string
  answer: string
  summary: string
}

export interface WorkbenchDocument {
  path: string
  title: string
  modifiedAt: string
}

export interface WorkbenchProgress {
  id: string
  taskId: string
  filePath: string
  taskTitle: string
  project: string
  note: string
  at: string
}

export interface WorkbenchTask {
  id: string
  filePath: string
  fileName: string
  lineIndex: number
  sourceLine: string
  content: string
  title: string
  type: 'US' | 'DTS' | '额外' | 'todo'
  project: string
  projectPath: string
  completed: boolean
  priority: string
  due: string
  date: string
  completedAt: string
  status: string
  blocker: string
  progress: WorkbenchProgress[]
}

export interface WorkbenchProject {
  name: string
  path: string
  folder: string
  status: string
  nextStep: string
  techStack: string[]
  taskIds: string[]
  notes: WorkbenchDocument[]
  modifiedAt: string
}

export interface WorkbenchBook {
  name: string
  path: string
  folder: string
  status: string
  progress: number
  projects: string[]
  chapters: WorkbenchDocument[]
  noteCount: number
  reviewCount: number
}

export interface InterviewCard {
  id: string
  filePath: string
  lineIndex: number
  comment: string
  question: string
  answer: string
  level: string
  interval: number
  due: string
  reps: number
  failures: number
  lastReviewed: string
  weak: boolean
}

export interface WorkbenchSnapshot {
  date: string
  tasks: WorkbenchTask[]
  projects: WorkbenchProject[]
  books: WorkbenchBook[]
  cards: InterviewCard[]
  progress: WorkbenchProgress[]
  radar: { path: string; content: string }
  documents: WorkbenchDocument[]
  indexedAt: string
  warnings: string[]
}

const service = 'github.com/notevault/notevault/internal/service.TodoService'

/** The existing registered TodoService owns the Markdown workbench domain. */
export const WorkbenchService = {
  DistillKnowledge(workspacePath: string, request: DistillRequest): Promise<void> {
    return Call.ByName(`${service}.DistillKnowledge`, workspacePath, request)
  },
  GetWorkbench(workspacePath: string, date: string): Promise<WorkbenchSnapshot> {
    return Call.ByName(`${service}.GetWorkbench`, workspacePath, date)
  },
  UpdateWorkbenchTask(
    workspacePath: string,
    filePath: string,
    lineIndex: number,
    expectedLine: string,
    action: 'toggle' | 'progress' | 'blocker',
    value: string,
    date: string,
  ): Promise<void> {
    return Call.ByName(`${service}.UpdateWorkbenchTask`, workspacePath, filePath, lineIndex, expectedLine, action, value, date)
  },
  ReviewInterviewCard(
    workspacePath: string,
    filePath: string,
    lineIndex: number,
    expectedComment: string,
    level: '掌握' | '模糊' | '不会',
    date: string,
  ): Promise<void> {
    return Call.ByName(`${service}.ReviewInterviewCard`, workspacePath, filePath, lineIndex, expectedComment, level, date)
  },
  ReadDailyReport(workspacePath: string, date: string): Promise<string> {
    return Call.ByName(`${service}.ReadDailyReport`, workspacePath, date)
  },
  SaveDailyReport(workspacePath: string, date: string, content: string, expectedContent: string): Promise<string> {
    return Call.ByName(`${service}.SaveDailyReport`, workspacePath, date, content, expectedContent)
  },
  AddWorkbenchTask(
    workspacePath: string,
    projectFolder: string,
    title: string,
    kind: string,
    due: string,
    date: string,
  ): Promise<void> {
    return Call.ByName(`${service}.AddWorkbenchTask`, workspacePath, projectFolder, title, kind, due, date)
  },
}
