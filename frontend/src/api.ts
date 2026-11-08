export type Tier = 'reference' | 'good' | 'anti' | 'early' | 'nodata' | 'pending'

export interface NavProject { code: string; name: string; openCandidates: number }
export interface Nav { readyToExport: number; projects: NavProject[] }

export interface Tiers { reference: number; good: number; anti: number; early: number; nodata: number }
export interface ProjectSummary {
  code: string; name: string; tiers: Tiers; hooks: number
  bestCpaCents: number | null; avgCpaCents: number | null; hookRate: number
  openCandidates: number; missingText: number
}
export interface Overview {
  openCandidates: number; missingText: number; readyToExport: number
  spendCents: number; avgCpaCents: number | null; references: number
  bestHook: { code: string; number: string; cpaCents: number } | null
  projects: ProjectSummary[]
}

export interface Settings {
  code: string; name: string; description: string; buyer: string
  minImpressions: number; minPurchases: number; maxCpaCents: number
  refSharePct: number; minRefs: number; maxRefs: number; maxAnti: number
  weightCpa: number; weightHookRate: number; weightPurchases: number; weightCtr: number
}

export interface HookRow {
  id: number; number: string; adName: string; text: string; onScreenText: string; type: string
  tier: Tier; reason: string; impressions: number; hookRate: number; purchases: number; cpaCents: number | null
}
export interface ProjectView {
  code: string; name: string; description: string; market: string; adNamePattern: string; settings: Settings
  spendCents: number; avgCpaCents: number | null; hookRate: number
  ranked: number; total: number; openCandidates: number
  hooks: HookRow[]; types: { type: string; cpaCents: number }[]
}

export interface ImportView {
  id: number; fileName: string; periodStart: string; periodEnd: string; createdAt: string
  rowsTotal: number; rowsMatched: number; hooksUpdated: number; hooksCreated: number
  skippedOtherProjects: number; skippedOtherMarkets: number
  unrecognized: { adName: string; project?: string; spendCents: number }[]
}

export interface Label { code: string; name: string }
export interface Reference { number: string; text: string; cpaCents: number }
export interface GenerateOptions {
  references: Reference[]; antiExamples: Reference[]; types: Label[]; untried: string[]
  settings: Settings; llmReady: boolean; needsBrief: boolean
}

export interface Candidate {
  id: number; text: string; onScreenText: string; type: string; mode: 'exploit' | 'explore' | ''
  basedOn: string; basedOnCpaCents: number | null; rationale: string
  status: 'candidate' | 'selected' | 'rejected' | 'exported'; number: string; adName: string
}
export interface CandidatesView {
  code: string; name: string; batchId: number; createdAt: string; requested: number
  dropped: { text: string; similar_to: string; similarity: number }[]
  nextNumber: string; items: Candidate[]
}

export type QueueStatus = 'ready' | 'waiting' | 'live'
export interface QueueItem { id: number; adName: string; text: string; onScreenText: string; type: string; note: string }
export interface QueueView { counts: Record<QueueStatus, number>; items: QueueItem[] }

export interface HookInput { text: string; onScreenText: string; adName?: string }

// codePattern matches project codes the server accepts, e.g. O12.
export const codePattern = /^[A-Z][A-Z0-9]{0,11}$/

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

async function failure(res: Response): Promise<never> {
  const data = await res.json().catch(() => null)
  const fallback = res.status >= 500 ? 'Something went wrong on our side. Try again in a moment.' : 'The request failed.'
  throw new ApiError(res.status, data?.error ?? fallback)
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method }
  if (body instanceof FormData) {
    init.body = body
  } else if (body !== undefined) {
    init.body = JSON.stringify(body)
    init.headers = { 'Content-Type': 'application/json' }
  }
  let res: Response
  try {
    res = await fetch('/api' + path, init)
  } catch {
    throw new ApiError(0, 'Cannot reach the server. Check that the app is running.')
  }
  if (!res.ok) return failure(res)
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  nav: () => request<Nav>('GET', '/nav'),
  overview: () => request<Overview>('GET', '/overview'),
  createProject: (body: { code: string; name: string; description: string; buyer: string }) => request<{ code: string }>('POST', '/projects', body),
  project: (code: string) => request<ProjectView>('GET', `/projects/${code}`),
  saveSettings: (code: string, s: Settings) => request<Settings>('PUT', `/projects/${code}/settings`, s),
  deleteProject: (code: string) => request<void>('DELETE', `/projects/${code}`),
  addHook: (code: string, h: HookInput) => request<{ adName: string }>('POST', `/projects/${code}/hooks`, h),
  updateHook: (id: number, h: HookInput) => request<void>('PUT', `/hooks/${id}`, h),
  deleteHook: (id: number) => request<void>('DELETE', `/hooks/${id}`),
  deleteHooks: (code: string, ids: number[]) => request<void>('POST', `/projects/${code}/hooks/delete`, { ids }),
  latestImport: (code: string) => request<ImportView>('GET', `/projects/${code}/import`),
  importMeta: (code: string, file: File) => {
    const form = new FormData()
    form.append('file', file)
    return request<ImportView>('POST', `/projects/${code}/import`, form)
  },
  assignAdNames: (code: string, items: { adName: string; hookId: number }[]) =>
    request<ImportView>('POST', `/projects/${code}/import/assign`, { items }),
  generateOptions: (code: string) => request<GenerateOptions>('GET', `/projects/${code}/generate`),
  generate: (code: string, body: { n: number; exploitPct: number; typeShares: Record<string, number>; brief: string }) =>
    request<CandidatesView>('POST', `/projects/${code}/generate`, body),
  candidates: (code: string) => request<CandidatesView>('GET', `/projects/${code}/candidates`),
  select: (id: number, h: HookInput) => request<void>('POST', `/candidates/${id}/select`, h),
  reject: (id: number) => request<void>('POST', `/candidates/${id}/reject`),
  undo: (id: number) => request<void>('POST', `/candidates/${id}/undo`),
  queue: (status: QueueStatus) => request<QueueView>('GET', `/queue?status=${status}`),
  exportCsv: async (ids: number[]) => {
    const res = await fetch('/api/queue/export', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids }),
    })
    if (!res.ok) return failure(res)
    return res.blob()
  },
}
