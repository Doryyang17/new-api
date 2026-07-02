/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'

import type {
  ConfirmPaymentComplianceResponse,
  CreateRegistrationCodesResponse,
  FetchUpstreamRatiosRequest,
  LogCleanupTask,
  PromptFilterApiResponse,
  PromptFilterLexiconFile,
  PromptFilterLexiconPreviewData,
  PromptFilterLexiconsData,
  PromptFilterLogsData,
  PromptFilterRulesData,
  PromptFilterStatusData,
  PromptFilterVerdict,
  RegistrationCodeListResponse,
  SystemOptionsResponse,
  SystemTaskListResponse,
  SystemTaskResponse,
  UpdateOptionRequest,
  UpdateOptionResponse,
  UpstreamChannelsResponse,
  UpstreamRatiosResponse,
} from './types'

export async function getSystemOptions() {
  const res = await api.get<SystemOptionsResponse>('/api/option/')
  return res.data
}

export async function updateSystemOption(request: UpdateOptionRequest) {
  const res = await api.put<UpdateOptionResponse>('/api/option/', request)
  return res.data
}

export async function getRegistrationCodes(
  status: 'unused' | 'used',
  page: number,
  pageSize: number
) {
  const res = await api.get<RegistrationCodeListResponse>(
    '/api/registration-code/',
    {
      params: { status, p: page, page_size: pageSize },
    }
  )
  return res.data
}

export async function createRegistrationCodes(request: {
  count: number
  note?: string
}) {
  const res = await api.post<CreateRegistrationCodesResponse>(
    '/api/registration-code/batch',
    request
  )
  return res.data
}

export async function confirmPaymentCompliance() {
  const res = await api.post<ConfirmPaymentComplianceResponse>(
    '/api/option/payment_compliance',
    { confirmed: true }
  )
  return res.data
}

export async function startLogCleanupTask(targetTimestamp: number) {
  const res = await api.post<SystemTaskResponse<LogCleanupTask>>(
    '/api/system-task/log-cleanup',
    null,
    {
      params: { target_timestamp: targetTimestamp },
    }
  )
  return res.data
}

export async function getCurrentLogCleanupTask() {
  const res = await api.get<SystemTaskResponse<LogCleanupTask | null>>(
    '/api/system-task/current',
    {
      params: { type: 'log_cleanup' },
    }
  )
  return res.data
}

export async function getSystemTask(taskId: string) {
  const res = await api.get<SystemTaskResponse<LogCleanupTask>>(
    `/api/system-task/${taskId}`
  )
  return res.data
}

export async function listSystemTasks(limit = 20) {
  const res = await api.get<SystemTaskListResponse>('/api/system-task/list', {
    params: { limit },
  })
  return res.data
}

export async function resetModelRatios() {
  const res = await api.post<UpdateOptionResponse>(
    '/api/option/rest_model_ratio'
  )
  return res.data
}

export async function getUpstreamChannels() {
  const res = await api.get<UpstreamChannelsResponse>(
    '/api/ratio_sync/channels'
  )
  return res.data
}

export async function fetchUpstreamRatios(request: FetchUpstreamRatiosRequest) {
  const res = await api.post<UpstreamRatiosResponse>(
    '/api/ratio_sync/fetch',
    request
  )
  return res.data
}

export async function getPromptFilterStatus() {
  const res = await api.get<PromptFilterApiResponse<PromptFilterStatusData>>(
    '/api/prompt-filter/status'
  )
  return res.data
}

export async function getPromptFilterRules() {
  const res = await api.get<PromptFilterApiResponse<PromptFilterRulesData>>(
    '/api/prompt-filter/rules'
  )
  return res.data
}

export async function getPromptFilterLexicons() {
  const res = await api.get<PromptFilterApiResponse<PromptFilterLexiconsData>>(
    '/api/prompt-filter/lexicons'
  )
  return res.data
}

export async function uploadPromptFilterLexicon(formData: FormData) {
  const res = await api.post<
    PromptFilterApiResponse<{ file: PromptFilterLexiconFile }>
  >('/api/prompt-filter/lexicons', formData)
  return res.data
}

export async function updatePromptFilterLexicon(request: {
  id: string
  enabled: boolean
}) {
  const res = await api.patch<
    PromptFilterApiResponse<{ file: PromptFilterLexiconFile }>
  >(`/api/prompt-filter/lexicons/${encodeURIComponent(request.id)}`, {
    enabled: request.enabled,
  })
  return res.data
}

export async function previewPromptFilterLexicon(request: {
  id: string
  limit?: number
}) {
  const res = await api.get<
    PromptFilterApiResponse<PromptFilterLexiconPreviewData>
  >(`/api/prompt-filter/lexicons/${encodeURIComponent(request.id)}/preview`, {
    params: { limit: request.limit ?? 200 },
  })
  return res.data
}

export async function savePromptFilterLexiconWords(request: {
  id: string
  words: string[]
}) {
  const res = await api.put<
    PromptFilterApiResponse<{ file: PromptFilterLexiconFile }>
  >(`/api/prompt-filter/lexicons/${encodeURIComponent(request.id)}/words`, {
    words: request.words,
  })
  return res.data
}

export async function deletePromptFilterLexicon(id: string) {
  const res = await api.delete<PromptFilterApiResponse<Record<string, never>>>(
    `/api/prompt-filter/lexicons/${encodeURIComponent(id)}`
  )
  return res.data
}

export async function getPromptFilterLogs(params: {
  page: number
  page_size: number
  source?: string
  action?: string
  endpoint?: string
  model?: string
  api_key_id?: string
  q?: string
}) {
  const res = await api.get<PromptFilterApiResponse<PromptFilterLogsData>>(
    '/api/prompt-filter/logs',
    { params }
  )
  return res.data
}

export async function clearPromptFilterLogs() {
  const res = await api.delete<
    PromptFilterApiResponse<{ deleted_count: number }>
  >('/api/prompt-filter/logs')
  return res.data
}

export async function testPromptFilter(request: {
  text: string
  endpoint: string
  model: string
}) {
  const res = await api.post<
    PromptFilterApiResponse<{ verdict: PromptFilterVerdict }>
  >('/api/prompt-filter/test', request)
  return res.data
}

export async function testPromptFilterRulePattern(request: {
  pattern: string
  text: string
}) {
  const res = await api.post<
    PromptFilterApiResponse<{ matched: boolean; error?: string }>
  >('/api/prompt-filter/rules/test', request)
  return res.data
}
