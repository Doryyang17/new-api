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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, FileText, RefreshCcw, Search, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatTimestampToDate } from '@/lib/format'

import {
  clearRequestRiskLogs,
  getRequestRiskLogDetail,
  getRequestRiskLogs,
} from '../api'
import type { RequestRiskLog } from '../types'

const PAGE_SIZE = 50

type RequestRiskLogFilters = {
  kind: string
  action: string
  level: string
  model: string
  apiKeyId: string
  query: string
}

const defaultFilters: RequestRiskLogFilters = {
  kind: 'all',
  action: 'all',
  level: 'all',
  model: '',
  apiKeyId: '',
  query: '',
}

const factorLabels: Record<string, string> = {
  temporary_block: '命中临时限制',
  burst_10s_high: '10 秒请求突增（高）',
  burst_10s: '10 秒请求突增',
  request_volume_60s_high: '60 秒请求量过高',
  request_volume_60s: '60 秒请求量偏高',
  ip_volume_60s_high: 'IP 60 秒请求量过高',
  ip_volume_60s: 'IP 60 秒请求量偏高',
  meaningless_exact_match: '无意义短文本精确命中',
  repeated_content_high: '重复内容过多（高）',
  repeated_content: '重复内容偏多',
  model_sweep_high: '批量轮询模型（高）',
  model_sweep: '批量轮询模型',
  failure_burst_high: '失败请求突增（高）',
  failure_burst: '失败请求偏多',
  fast_failure_retry: '失败后快速重试',
  user_concurrency_limit: '用户在途并发超限',
  token_concurrency_limit: 'API 密钥在途并发超限',
}

function factorLabel(factor: string) {
  return factorLabels[factor] ?? factor
}

function kindLabel(kind: RequestRiskLog['kind']) {
  return kind === 'concurrency' ? '并发保护' : '批量测活'
}

function actionLabel(log: RequestRiskLog) {
  return log.blocked ? '已拦截' : '仅观察'
}

function userLabel(log: RequestRiskLog) {
  if (log.username && log.user_id > 0) {
    return `${log.username} (#${log.user_id})`
  }
  if (log.username) {
    return log.username
  }
  return log.user_id > 0 ? `#${log.user_id}` : '-'
}

function apiKeyLabel(log: RequestRiskLog) {
  if (log.api_key_name && log.api_key_id > 0) {
    return `${log.api_key_name} (#${log.api_key_id})`
  }
  if (log.api_key_name) {
    return log.api_key_name
  }
  return log.api_key_id > 0 ? `#${log.api_key_id}` : '-'
}

function riskLevelLabel(level: string) {
  if (level === 'high') return '高风险'
  if (level === 'medium') return '中风险'
  if (level === 'low') return '低风险'
  return '-'
}

export function RequestRiskLogsPanel() {
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState(defaultFilters)
  const [appliedFilters, setAppliedFilters] = useState(defaultFilters)
  const [clearConfirmOpen, setClearConfirmOpen] = useState(false)
  const logsQuery = useQuery({
    queryKey: ['request-risk-logs', page, appliedFilters],
    queryFn: async () => {
      const response = await getRequestRiskLogs({
        page,
        page_size: PAGE_SIZE,
        kind: appliedFilters.kind === 'all' ? undefined : appliedFilters.kind,
        action:
          appliedFilters.action === 'all' ? undefined : appliedFilters.action,
        level:
          appliedFilters.level === 'all' ? undefined : appliedFilters.level,
        model: appliedFilters.model.trim() || undefined,
        api_key_id: appliedFilters.apiKeyId.trim() || undefined,
        q: appliedFilters.query.trim() || undefined,
      })
      if (!response.success) {
        throw new Error(response.message || '风控日志加载失败')
      }
      return response
    },
  })
  const clearLogsMutation = useMutation({
    mutationFn: clearRequestRiskLogs,
    onSuccess: async (response) => {
      if (!response.success) {
        toast.error(response.message || '风控日志清理失败')
        return
      }
      setPage(1)
      setClearConfirmOpen(false)
      await queryClient.invalidateQueries({ queryKey: ['request-risk-logs'] })
      toast.success(`已清理 ${response.data.deleted_count} 条风控日志`)
    },
    onError: (error: Error) => {
      toast.error(error.message || '风控日志清理失败')
    },
  })

  const rows = logsQuery.data?.data.items ?? []
  const total = logsQuery.data?.data.total ?? 0
  const canPrevious = page > 1
  const canNext = page * PAGE_SIZE < total

  const applyFilters = () => {
    setPage(1)
    setAppliedFilters(filters)
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>触发日志</CardTitle>
          <CardDescription>
            查看批量测活与并发保护的命中项、评分文本和管理员可见的完整请求体。
          </CardDescription>
          <CardAction className='flex gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => logsQuery.refetch()}
              disabled={logsQuery.isFetching}
            >
              <RefreshCcw data-icon='inline-start' />
              刷新
            </Button>
            <Button
              type='button'
              variant='destructive'
              size='sm'
              onClick={() => setClearConfirmOpen(true)}
              disabled={clearLogsMutation.isPending}
            >
              <Trash2 data-icon='inline-start' />
              清空全部日志
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className='space-y-4'>
          <div className='grid gap-3 md:grid-cols-3 xl:grid-cols-6'>
            <Select
              value={filters.kind}
              onValueChange={(value) =>
                setFilters((current) => ({ ...current, kind: value ?? 'all' }))
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value='all'>全部类型</SelectItem>
                <SelectItem value='probe'>批量测活</SelectItem>
                <SelectItem value='concurrency'>并发保护</SelectItem>
              </SelectContent>
            </Select>
            <Select
              value={filters.action}
              onValueChange={(value) =>
                setFilters((current) => ({
                  ...current,
                  action: value ?? 'all',
                }))
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value='all'>全部处置</SelectItem>
                <SelectItem value='observed'>仅观察</SelectItem>
                <SelectItem value='blocked'>已拦截</SelectItem>
              </SelectContent>
            </Select>
            <Select
              value={filters.level}
              onValueChange={(value) =>
                setFilters((current) => ({ ...current, level: value ?? 'all' }))
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value='all'>全部风险等级</SelectItem>
                <SelectItem value='medium'>中风险</SelectItem>
                <SelectItem value='high'>高风险</SelectItem>
              </SelectContent>
            </Select>
            <Input
              value={filters.model}
              placeholder='模型'
              onChange={(event) =>
                setFilters((current) => ({
                  ...current,
                  model: event.target.value,
                }))
              }
            />
            <Input
              value={filters.apiKeyId}
              placeholder='API 密钥 ID'
              inputMode='numeric'
              onChange={(event) =>
                setFilters((current) => ({
                  ...current,
                  apiKeyId: event.target.value,
                }))
              }
            />
            <div className='flex gap-2'>
              <Input
                value={filters.query}
                placeholder='关键词或用户名'
                onChange={(event) =>
                  setFilters((current) => ({
                    ...current,
                    query: event.target.value,
                  }))
                }
                onKeyDown={(event) => {
                  if (event.key === 'Enter') applyFilters()
                }}
              />
              <Button
                type='button'
                size='icon'
                aria-label='查询风控日志'
                onClick={applyFilters}
              >
                <Search />
              </Button>
            </div>
          </div>

          <RequestRiskLogsTable
            isError={logsQuery.isError}
            error={logsQuery.error}
            rows={rows}
          />

          <div className='flex items-center justify-between gap-3'>
            <div className='text-muted-foreground text-sm'>共 {total} 条</div>
            <div className='flex gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={!canPrevious}
                onClick={() => setPage((current) => current - 1)}
              >
                上一页
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={!canNext}
                onClick={() => setPage((current) => current + 1)}
              >
                下一页
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={clearConfirmOpen}
        onOpenChange={(open) => {
          if (!clearLogsMutation.isPending) setClearConfirmOpen(open)
        }}
        title='确认清空全部风控日志'
        desc='此操作会忽略当前筛选条件，永久删除全部批量测活与并发保护日志，且无法恢复。'
        confirmText={
          clearLogsMutation.isPending ? '正在清理' : '确认清空全部日志'
        }
        destructive
        isLoading={clearLogsMutation.isPending}
        handleConfirm={() => clearLogsMutation.mutate()}
      />
    </>
  )
}

function RequestRiskLogsTable(props: {
  isError: boolean
  error: Error | null
  rows: RequestRiskLog[]
}) {
  if (props.isError) {
    return (
      <Alert variant='destructive'>
        <AlertTitle>风控日志加载失败</AlertTitle>
        <AlertDescription>
          {props.error?.message || '请稍后重试'}
        </AlertDescription>
      </Alert>
    )
  }
  if (props.rows.length === 0) {
    return (
      <div className='grid min-h-44 place-items-center rounded-lg border border-dashed'>
        <div className='text-center'>
          <FileText className='text-muted-foreground mx-auto mb-2 size-6' />
          <div className='font-medium'>暂无触发记录</div>
          <div className='text-muted-foreground text-sm'>
            当前筛选条件下没有可展示的风控日志。
          </div>
        </div>
      </div>
    )
  }
  return (
    <div className='overflow-x-auto'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>处置</TableHead>
            <TableHead className='whitespace-nowrap'>时间</TableHead>
            <TableHead className='whitespace-nowrap'>触发用户</TableHead>
            <TableHead>类型</TableHead>
            <TableHead>模型</TableHead>
            <TableHead>风险</TableHead>
            <TableHead className='min-w-64'>命中项</TableHead>
            <TableHead className='min-w-80'>请求预览</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.rows.map((log) => (
            <RequestRiskLogRow key={log.id} log={log} />
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function RequestRiskLogRow({ log }: { log: RequestRiskLog }) {
  return (
    <TableRow>
      <TableCell>
        <Badge variant={log.blocked ? 'destructive' : 'outline'}>
          {actionLabel(log)}
        </Badge>
      </TableCell>
      <TableCell className='font-mono text-xs whitespace-nowrap'>
        {formatTimestampToDate(log.created_at)}
      </TableCell>
      <TableCell className='max-w-48 truncate'>{userLabel(log)}</TableCell>
      <TableCell>{kindLabel(log.kind)}</TableCell>
      <TableCell className='max-w-48 truncate'>{log.model || '-'}</TableCell>
      <TableCell className='whitespace-nowrap'>
        {log.risk_level
          ? `${riskLevelLabel(log.risk_level)} / ${log.score}`
          : '-'}
      </TableCell>
      <TableCell className='whitespace-normal'>
        <div className='flex flex-wrap gap-1'>
          {log.factors.length > 0 ? (
            log.factors.map((factor) => (
              <Badge key={factor} variant='secondary'>
                {factorLabel(factor)}
              </Badge>
            ))
          ) : (
            <span>-</span>
          )}
          {log.matched_keywords.map((keyword) => (
            <Badge key={`keyword-${keyword}`} variant='outline'>
              关键词：{keyword}
            </Badge>
          ))}
        </div>
      </TableCell>
      <TableCell className='whitespace-normal'>
        <div className='space-y-1.5'>
          <div className='line-clamp-2'>{log.text_preview || '-'}</div>
          <RequestRiskLogDetailsDialog log={log} />
        </div>
      </TableCell>
    </TableRow>
  )
}

function RequestRiskLogDetailsDialog({ log }: { log: RequestRiskLog }) {
  const [open, setOpen] = useState(false)
  const detailQuery = useQuery({
    queryKey: [
      'request-risk-log-detail',
      log.request_id,
      log.kind,
      log.created_at,
    ],
    queryFn: async () => {
      const response = await getRequestRiskLogDetail({
        request_id: log.request_id,
        kind: log.kind,
        created_at: log.created_at,
      })
      if (!response.success || !response.data) {
        throw new Error(response.message || '风控日志详情加载失败')
      }
      return response.data
    },
    enabled: open && log.request_id !== '' && log.created_at > 0,
    staleTime: 5 * 60 * 1000,
  })
  const detail = detailQuery.data
  let extractedTextContent = (
    <pre className='bg-muted max-h-64 overflow-auto rounded-lg border p-3 text-xs break-words whitespace-pre-wrap'>
      {detail?.extracted_text || log.text_preview || '-'}
    </pre>
  )
  if (detailQuery.isFetching) {
    extractedTextContent = (
      <Alert>
        <AlertTitle>正在加载完整内容</AlertTitle>
        <AlertDescription>请稍候。</AlertDescription>
      </Alert>
    )
  } else if (detailQuery.isError) {
    extractedTextContent = (
      <Alert variant='destructive'>
        <AlertTitle>完整内容加载失败</AlertTitle>
        <AlertDescription>
          {detailQuery.error.message || '请稍后重试'}
        </AlertDescription>
      </Alert>
    )
  }

  let fullRequestContent = (
    <Alert>
      <AlertTitle>本条日志没有完整请求体</AlertTitle>
      <AlertDescription>
        {detail?.full_request_unavailable_reason ||
          log.full_request_unavailable_reason ||
          '请求未包含可记录的完整请求体。'}
      </AlertDescription>
    </Alert>
  )
  if (detailQuery.isFetching) {
    fullRequestContent = (
      <Alert>
        <AlertTitle>正在加载完整请求体</AlertTitle>
        <AlertDescription>请稍候。</AlertDescription>
      </Alert>
    )
  } else if (detailQuery.isError) {
    fullRequestContent = (
      <Alert variant='destructive'>
        <AlertTitle>完整请求体加载失败</AlertTitle>
        <AlertDescription>
          {detailQuery.error.message || '请稍后重试'}
        </AlertDescription>
      </Alert>
    )
  } else if (detail?.full_request_available && detail.full_request) {
    fullRequestContent = (
      <pre className='bg-muted max-h-96 overflow-auto rounded-lg border p-3 text-xs break-words whitespace-pre-wrap'>
        {detail.full_request}
      </pre>
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title='风控触发日志详情'
      description='查看命中项、风控计数、完整评分文本和管理员可见的完整请求体。'
      trigger={
        <Button
          type='button'
          variant='ghost'
          size='sm'
          className='text-muted-foreground h-6 px-1.5'
          aria-label={`查看 ${formatTimestampToDate(log.created_at)} 风控日志完整信息`}
        >
          <Eye data-icon='inline-start' />
          查看完整
        </Button>
      }
      contentClassName='sm:max-w-5xl'
      bodyClassName='space-y-5'
    >
      <div className='flex flex-wrap gap-2'>
        <Badge variant={log.blocked ? 'destructive' : 'outline'}>
          {actionLabel(log)}
        </Badge>
        <Badge variant='secondary'>{kindLabel(log.kind)}</Badge>
        {log.risk_level ? (
          <Badge
            variant={log.risk_level === 'high' ? 'destructive' : 'outline'}
          >
            {riskLevelLabel(log.risk_level)}
          </Badge>
        ) : null}
      </div>

      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
        <DetailField
          label='时间'
          value={formatTimestampToDate(log.created_at)}
          mono
        />
        <DetailField label='触发用户' value={userLabel(log)} />
        <DetailField label='API 密钥' value={apiKeyLabel(log)} mono />
        <DetailField label='端点' value={log.endpoint || '-'} mono />
        <DetailField label='模型' value={log.model || '-'} mono />
        <DetailField label='分组' value={log.group || '-'} mono />
        <DetailField
          label='运行模式'
          value={log.mode === 'enforce' ? '拦截模式' : '观察模式'}
        />
        <DetailField label='风险分' value={String(log.score)} mono />
        <DetailField label='客户端 IP' value={log.client_ip || '-'} mono />
        <DetailField label='请求 ID' value={log.request_id || '-'} mono wide />
        <DetailField
          label='提取字符数'
          value={String(log.extracted_chars)}
          mono
        />
      </div>

      <div className='space-y-2'>
        <div className='text-sm font-medium'>命中项</div>
        <div className='flex flex-wrap gap-2 rounded-lg border p-3'>
          {log.factors.length > 0 ? (
            log.factors.map((factor) => (
              <Badge key={factor} variant='secondary'>
                {factorLabel(factor)}
              </Badge>
            ))
          ) : (
            <span className='text-muted-foreground text-sm'>无</span>
          )}
        </div>
      </div>

      {log.matched_keywords.length > 0 ? (
        <div className='space-y-2'>
          <div className='text-sm font-medium'>命中关键词</div>
          <div className='flex flex-wrap gap-2 rounded-lg border p-3'>
            {log.matched_keywords.map((keyword) => (
              <Badge key={keyword} variant='outline'>
                {keyword}
              </Badge>
            ))}
          </div>
        </div>
      ) : null}

      <div className='space-y-2'>
        <div className='text-sm font-medium'>风控计数</div>
        <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
          {log.kind === 'probe' ? (
            <>
              <DetailField
                label='10 秒请求数'
                value={String(log.request_count_10s)}
                mono
              />
              <DetailField
                label='60 秒请求数'
                value={String(log.request_count_60s)}
                mono
              />
              <DetailField
                label='IP 60 秒请求数'
                value={String(log.ip_request_count_60s)}
                mono
              />
              <DetailField
                label='60 秒重复次数'
                value={String(log.repeat_count_60s)}
                mono
              />
              <DetailField
                label='60 秒模型数'
                value={String(log.distinct_models_60s)}
                mono
              />
              <DetailField
                label='30 秒失败数'
                value={String(log.failure_count_30s)}
                mono
              />
            </>
          ) : (
            <>
              <DetailField
                label='用户在途请求'
                value={`${log.user_in_flight}/${log.user_limit}`}
                mono
              />
              <DetailField
                label='密钥在途请求'
                value={`${log.token_in_flight}/${log.token_limit}`}
                mono
              />
            </>
          )}
        </div>
      </div>

      <div className='space-y-2'>
        <div className='text-sm font-medium'>评分文本预览</div>
        <pre className='bg-muted max-h-40 overflow-auto rounded-lg border p-3 text-xs break-words whitespace-pre-wrap'>
          {log.text_preview || '-'}
        </pre>
      </div>

      <div className='space-y-2'>
        <div className='text-sm font-medium'>完整评分文本</div>
        {extractedTextContent}
      </div>

      <div className='space-y-2'>
        <div className='text-sm font-medium'>完整请求体</div>
        {fullRequestContent}
      </div>
    </Dialog>
  )
}

function DetailField({
  label,
  value,
  mono = false,
  wide = false,
}: {
  label: string
  value: string
  mono?: boolean
  wide?: boolean
}) {
  return (
    <div className={wide ? 'space-y-1 lg:col-span-2' : 'space-y-1'}>
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div
        className={mono ? 'font-mono text-sm break-all' : 'text-sm break-words'}
      >
        {value}
      </div>
    </div>
  )
}
