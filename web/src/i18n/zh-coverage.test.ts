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
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import {
  TICKET_CATEGORIES,
  TICKET_PRIORITIES,
  TICKET_STATUSES,
} from '@/features/tickets/types'
import { SIDEBAR_MODULES_META } from '@/lib/sidebar-modules'

import zhCN from './locales/zh.json'
import { STATIC_I18N_KEYS } from './static-keys'

const SRC_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

const NO_HARDCODED_CHINESE_FILES = [
  'features/admin-referral/index.tsx',
  'features/system-settings/content/announcements-section.tsx',
  'features/auth/sign-up/components/sign-up-form.tsx',
  'features/auth/sign-in/components/user-auth-form.tsx',
  'features/auth/components/legal-consent.tsx',
  'features/system-settings/integrations/monitoring-settings-section.tsx',
  'features/keys/components/api-key-usage-cell.tsx',
  'features/system-settings/integrations/payment-method-dialog.tsx',
  'features/users/components/users-columns.tsx',
  'components/layout/components/footer.tsx',
  'features/system-settings/operations/index.tsx',
  'features/system-settings/components/settings-page.tsx',
  'features/system-settings/content/index.tsx',
  'features/system-settings/auth/custom-oauth/types.ts',
  'features/wallet/lib/billing.ts',
  'features/dashboard/lib/flow.ts',
  'features/dashboard/lib/charts.ts',
  'features/channels/lib/channel-utils.ts',
  'features/channels/lib/model-mapping-validation.ts',
  'lib/passkey.ts',
  'features/tickets/api.ts',
  'features/channels/lib/channel-form.ts',
  'features/users/components/data-table-row-actions.tsx',
  'components/data-table/data-table-page.tsx',
  'features/channels/components/drawers/channel-mutate-drawer.tsx',
]

const KEEP_ENGLISH = new Set([
  'AI Proxy',
  'AIGC2D',
  'Alipay',
  'Anthropic',
  'API URL',
  'API2GPT',
  'AccessKey / SecretAccessKey',
  'AZURE_OPENAI_ENDPOINT *',
  'Baidu V2',
  'Bark',
  'ChatGPT',
  'Claude',
  'Client ID',
  'Client Secret',
  'Cloudflare',
  'Cohere',
  'DeepSeek',
  'Discord',
  'DoubaoVideo',
  'FastGPT',
  'Gemini',
  'Gemini Image 4K',
  'GitHub',
  'Gotify',
  'Jimeng',
  'JustSong',
  'LingYiWanWu',
  'LinuxDO',
  'Midjourney',
  'MidjourneyPlus',
  'Midjourney-Proxy',
  'MiniMax',
  'Mistral',
  'MokaAI',
  'Moonshot',
  'New API',
  'NewAPI',
  'OAuth',
  'OAuth Client Secret',
  'OhMyGPT',
  'Ollama',
  'One API',
  'OpenAI',
  'OpenAIMax',
  'OpenRouter',
  'Pancake',
  'Passkey',
  'Perplexity',
  'Polygon',
  'QuantumNous',
  'Replicate',
  'SiliconFlow',
  'Stripe',
  'Submodel',
  'SunoAPI',
  'Telegram',
  'Tencent',
  'TTFT P50',
  'TTFT P95',
  'TTFT P99',
  'USDT',
  'Uptime Kuma',
  'Uptime Kuma URL',
  'Vertex AI',
  'VolcEngine',
  'WeChat',
  'WeChat Pay',
  'Webhook',
  'Webhook URL',
  'Well-Known URL',
  'Worker URL',
  'Xinference',
  'Xunfei',
  'Zhipu V4',
  'Creem Product ID',
  'Stripe Price ID',
  'Waffo Pancake Product ID',
  'OpenAI Chat',
  'OpenAI Responses',
  'ChatGPT Subscription (Codex)',
  'OpenAI Responses Compact',
  'Claude Messages',
  'Official OpenAI Responses',
  'Official Claude Messages',
  'CC Switch',
  'Chat ID',
  'Codex Alpha Search',
  'CPU',
  'Gemini Batch Embed Contents',
  'Gemini Embed Content',
  'Gemini Generate Content',
  'Grok',
  'HTTP/1.1',
  'JSON',
  'OIDC',
  'OpenAI Audio Speech',
  'OpenAI Audio Transcriptions',
  'OpenAI Audio Translations',
  'OpenAI Completions',
  'OpenAI Embeddings',
  'OpenAI Image Edits',
  'OpenAI Image Generations',
  'OpenAI Models',
  'OpenAI Realtime',
  'Plan ID',
  'RPM',
  'SSL/TLS',
  'STARTTLS',
  'Source ID',
  'Token ID',
  'TPM',
  'TTL',
  'URL',
  'USD',
  'UTC',
  'Waffo',
  'Waffo Pancake MoR',
  'CNY (¥)',
  'USD ($)',
  'EUR (€)',
  'US dollar (USD)',
  'New API &lt;noreply@example.com&gt;',
  'Sans',
  'Serif',
  'edit_this',
  'vip',
  '"default": "us-central1", "claude-3-5-sonnet-20240620": "europe-west1"',
])

function walk(dir: string, acc: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      if (['node_modules', 'dist', 'locales'].includes(entry.name)) continue
      walk(full, acc)
      continue
    }
    if (!/\.(tsx|ts|jsx|js)$/.test(entry.name)) continue
    if (/\.(test|spec)\.(tsx|ts|jsx|js)$/.test(entry.name)) continue
    acc.push(full)
  }
  return acc
}

function stripComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|[^:\\\n])\/\/.*$/gm, '$1')
}

function extractLiteralTKeys(source: string): string[] {
  const keys: string[] = []
  const cleaned = stripComments(source)
  const re = /\b(?:i18n\.)?t\(\s*(['"])((?:\\.|[^\\])*?)\1/g
  let match: RegExpExecArray | null
  while ((match = re.exec(cleaned))) {
    keys.push(match[2].replaceAll('\\n', '\n').replaceAll("\\'", "'"))
  }
  return keys
}

function extractConfigKeys(source: string, field: 'titleKey' | 'labelKey'): string[] {
  const keys: string[] = []
  const cleaned = stripComments(source)
  const re = new RegExp(`${field}:\\s*(['"])((?:\\\\.|[^\\\\])*?)\\1`, 'g')
  let match: RegExpExecArray | null
  while ((match = re.exec(cleaned))) {
    keys.push(match[2].replaceAll('\\n', '\n').replaceAll("\\'", "'"))
  }
  return keys
}

function shouldStayEnglish(key: string): boolean {
  const value = key.trim()
  if (KEEP_ENGLISH.has(value)) return true
  if (!/[A-Za-z]{2,}/.test(value)) return true
  if (/[\u4e00-\u9fff]/.test(value)) return true
  if (/^https?:\/\//.test(value) || /^\/[\w/-]+/.test(value)) return true
  if (/^[\w.-]+@[\w.-]+$/.test(value)) return true
  if (/^smtp\./i.test(value) || /^socks5:/i.test(value)) return true
  if (/^gpt-/i.test(value) || value.startsWith('org-') || value.startsWith('price_') || value.startsWith('whsec_')) {
    return true
  }
  if (value.startsWith('checkout.') || value.startsWith('footer.')) return true
  if ((value.startsWith('{') && !value.startsWith('{{')) || value.startsWith('[') || value.includes('&#10;')) {
    return true
  }
  if (/^[A-Z0-9_ *./:-]+$/.test(value)) return true
  return false
}

describe('zhCN console copy coverage', () => {
  const zh = (zhCN as { translation: Record<string, string> }).translation
  const files = walk(SRC_DIR)
  const used = new Set<string>()
  const titleKeys = new Set<string>()

  for (const file of files) {
    const source = fs.readFileSync(file, 'utf8')
    for (const key of extractLiteralTKeys(source)) used.add(key)
    for (const key of extractConfigKeys(source, 'titleKey')) {
      used.add(key)
      titleKeys.add(key)
    }
    for (const key of extractConfigKeys(source, 'labelKey')) used.add(key)
  }
  for (const key of STATIC_I18N_KEYS) used.add(key)
  for (const key of [
    ...TICKET_CATEGORIES,
    ...TICKET_PRIORITIES,
    ...TICKET_STATUSES,
  ]) {
    used.add(key)
  }
  for (const section of Object.values(SIDEBAR_MODULES_META)) {
    used.add(section.title)
    used.add(section.description)
    for (const moduleMeta of Object.values(section.modules)) {
      used.add(moduleMeta.title)
      used.add(moduleMeta.description)
    }
  }

  it('keeps STATIC_I18N_KEYS unique', () => {
    expect([...STATIC_I18N_KEYS]).toEqual([...new Set(STATIC_I18N_KEYS)])
  })

  it('uses English titleKey literals so English UI does not leak Chinese', () => {
    const chineseTitleKeys = [...titleKeys]
      .filter((key) => /[\u4e00-\u9fff]/.test(key))
      .sort()
    expect(chineseTitleKeys).toEqual([])
  })

  it('keeps SIDEBAR_MODULES_META titles and descriptions as English keys', () => {
    const chinese: string[] = []
    for (const section of Object.values(SIDEBAR_MODULES_META)) {
      for (const value of [
        section.title,
        section.description,
        ...Object.values(section.modules).flatMap((moduleMeta) => [
          moduleMeta.title,
          moduleMeta.description,
        ]),
      ]) {
        if (/[\u4e00-\u9fff]/.test(value)) chinese.push(value)
      }
    }
    expect(chinese).toEqual([])
  })

  it('registers every static t() key in zh.json', () => {
    const missing = [...used].filter((key) => !(key in zh)).sort()
    expect(missing).toEqual([])
  })

  it('uses Chinese values for translatable console copy', () => {
    const stillEnglish = [...used]
      .filter((key) => !shouldStayEnglish(key))
      .filter((key) => {
        const value = zh[key]
        return typeof value !== 'string' || !/[\u4e00-\u9fff]/.test(value)
      })
      .sort()

    expect(stillEnglish).toEqual([])
  })

  it('uses agreed Chinese wording for known awkward keys', () => {
    expect(zh.Playground).toBe('操练场')
    expect(zh.to).toBe('至')
    expect(zh['(ID: {{id}})']).toBe('（编号 {{id}}）')
    expect(zh['GPT Image Workbench']).toBe('GPT生图工作台')
    expect(zh['Model Status Monitor']).toBe('模型状态监测')
    expect(zh['Ticket Center']).toBe('工单中心')
    expect(zh['Ticket Management']).toBe('工单管理')
    expect(zh['Telegram Push']).toBe('Telegram 推送')
    expect(zh['Ticket Notifications']).toBe('工单通知')
    expect(zh['Delivered Content']).toBe('交付内容')
    expect(zh['Subscription plan: {{name}}']).toBe('订阅套餐: {{name}}')
    expect(zh['Subscription service: activated']).toBe('订阅服务: 已开通')
    expect(zh['Balance top-up: {{amount}}']).toBe('余额充值: {{amount}}')
    expect(zh['CNY equivalent: {{amount}}']).toBe('折合人民币: {{amount}}')
    expect(zh['Alipay Blue']).toBe('支付宝蓝')
    expect(zh['WeChat Green']).toBe('微信绿')

    const promotionsKeys = [
      'If this email is registered, a password reset email will be sent. Please check your inbox, spam, or promotions folder.',
      'This email is used to receive verification codes. Verification email delivery may be briefly delayed. Please check your inbox, spam, or promotions folder.',
      'Verification code sent. If it does not arrive, check spam or promotions, or try again later.',
    ]
    for (const key of promotionsKeys) {
      expect(zh[key], key).toContain('促销')
      expect(zh[key], key).not.toContain('推广文件夹')
    }

    const referralPathKey =
      'Referral links will open this internal frontend path with ?aff=invite_code appended. Use /sign-up for the default template. /register is only kept as a legacy compatibility redirect.'
    expect(zh[referralPathKey]).toContain('站内前端路径')
    expect(zh[referralPathKey]).not.toContain('后端前端')

    expect(zh['Last Active']).toBe('最近活跃')
    expect(zh['This Month']).toBe('本月')
    expect(zh['Last used']).toBe('最后使用')
    expect(zh['Reason / Error']).toBe('原因/错误')
    expect(zh['Loading operations settings...']).toBe('正在加载运维设置...')
    expect(zh['Loading console content settings...']).toBe(
      '正在加载控制台内容设置...'
    )
    expect(zh['Please read and agree to the agreements first']).toBe(
      '请先阅读并同意协议'
    )
    expect(zh['Table horizontal scrollbar']).toBe('表格横向滚动条')
    expect(zh['Admin operation']).toBe('管理员操作')
    expect(zh['Retry commission generation']).toBe('重试生成佣金')
  })

  it('does not hardcode Chinese console copy in remaining i18n files', () => {
    const leftover: string[] = []
    const tCall =
      /\b(?:i18n\.)?t\(\s*(['"])(?:\\.|[^\\])*?\1/g
    for (const rel of NO_HARDCODED_CHINESE_FILES) {
      const full = path.join(SRC_DIR, rel)
      const source = stripComments(fs.readFileSync(full, 'utf8')).replace(
        tCall,
        't()'
      )
      source.split('\n').forEach((line, index) => {
        if (/[\u4e00-\u9fff]/.test(line)) {
          leftover.push(`${rel}:${index + 1}:${line.trim()}`)
        }
      })
    }
    expect(leftover).toEqual([])
  })
})
