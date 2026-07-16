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
import { Clock3, Moon, ShieldCheck } from 'lucide-react'

import { getSystemCurfewMessage } from '@/lib/system-availability'

export function CurfewHome() {
  return (
    <main className='relative isolate flex min-h-screen items-center overflow-hidden px-4 pt-20 pb-12'>
      <div className='absolute inset-0 -z-10 bg-[radial-gradient(circle_at_12%_12%,rgba(96,165,250,0.32),transparent_34%),radial-gradient(circle_at_82%_10%,rgba(45,212,191,0.24),transparent_32%),radial-gradient(circle_at_52%_78%,rgba(167,139,250,0.22),transparent_38%),linear-gradient(135deg,#f8fbff_0%,#eef7ff_44%,#f7f3ff_100%)] dark:bg-[radial-gradient(circle_at_16%_16%,rgba(37,99,235,0.34),transparent_34%),radial-gradient(circle_at_82%_12%,rgba(20,184,166,0.24),transparent_32%),radial-gradient(circle_at_54%_78%,rgba(124,58,237,0.26),transparent_40%),linear-gradient(135deg,#020617_0%,#08111f_46%,#111027_100%)]' />
      <div className='from-background/0 via-background/40 to-background absolute inset-x-0 bottom-0 -z-10 h-44 bg-gradient-to-b' />
      <div className='mx-auto grid w-full max-w-6xl items-center gap-10 lg:grid-cols-[1.05fr_0.95fr]'>
        <section className='space-y-7'>
          <div className='border-primary/20 bg-background/80 text-primary inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm font-medium shadow-sm backdrop-blur'>
            <Moon className='size-4' aria-hidden='true' />
            系统暂不可用
          </div>

          <div className='space-y-5'>
            <h1 className='max-w-3xl text-4xl leading-tight font-semibold tracking-normal text-balance sm:text-5xl lg:text-6xl'>
              <span className='block'>服务暂时进入</span>
              <span className='from-primary block bg-gradient-to-r via-violet-500 to-emerald-500 bg-clip-text text-transparent'>
                宵禁状态
              </span>
            </h1>
            <p className='text-muted-foreground max-w-2xl text-lg leading-8 sm:text-xl'>
              {getSystemCurfewMessage()}
            </p>
          </div>

          <div className='text-muted-foreground flex flex-wrap items-center gap-3 text-sm'>
            <span className='bg-background/75 border-border inline-flex items-center gap-2 rounded-full border px-3 py-1.5 shadow-sm backdrop-blur'>
              <Clock3 className='size-4 text-amber-500' aria-hidden='true' />
              API 调用已暂停
            </span>
            <span className='bg-background/75 border-border inline-flex items-center gap-2 rounded-full border px-3 py-1.5 shadow-sm backdrop-blur'>
              <ShieldCheck
                className='size-4 text-emerald-500'
                aria-hidden='true'
              />
              前端页面仍可浏览
            </span>
          </div>
        </section>

        <section className='border-border/70 bg-background/80 rounded-2xl border p-6 shadow-[0_24px_80px_-40px_rgba(15,23,42,0.45)] backdrop-blur-xl dark:shadow-[0_24px_80px_-44px_rgba(0,0,0,0.8)]'>
          <div className='border-border/70 rounded-xl border bg-white/70 p-5 dark:bg-white/[0.03]'>
            <div className='mb-5 flex items-center justify-between'>
              <div>
                <p className='text-sm font-medium'>Gateway status</p>
                <p className='text-muted-foreground mt-1 text-xs'>
                  Request guard is active
                </p>
              </div>
              <span className='rounded-full bg-amber-500/15 px-3 py-1 text-xs font-medium text-amber-700 dark:text-amber-300'>
                503
              </span>
            </div>
            <div className='space-y-3 font-mono text-sm'>
              <div className='border-border/70 bg-muted/60 rounded-lg border p-4'>
                <span className='text-emerald-600 dark:text-emerald-400'>
                  POST
                </span>
                <span className='text-muted-foreground'>
                  {' '}
                  /v1/chat/completions
                </span>
              </div>
              <div className='border-border/70 bg-muted/60 text-muted-foreground rounded-lg border p-4'>
                <span>{'{'}</span>
                <div className='pl-4'>
                  "code": "system_curfew",
                  <br />
                  "message": "当前处于宵禁状态"
                </div>
                <span>{'}'}</span>
              </div>
            </div>
          </div>
        </section>
      </div>
    </main>
  )
}
