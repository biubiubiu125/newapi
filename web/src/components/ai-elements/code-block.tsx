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
/* eslint-disable react-refresh/only-export-components */
'use client'

import {
  CheckIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  CopyIcon,
} from 'lucide-react'
import {
  type ComponentProps,
  createContext,
  type HTMLAttributes,
  type ReactNode,
  useContext,
  useEffect,
  useState,
} from 'react'
import {
  type BundledLanguage,
  codeToHtml,
  type ShikiTransformer,
} from 'shiki/bundle/web'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type CodeBlockProps = Omit<HTMLAttributes<HTMLDivElement>, 'title'> & {
  collapsedLines?: number
  code: string
  defaultCollapsed?: boolean
  language: BundledLanguage | string
  maxExpandedLines?: number
  showLineNumbers?: boolean
  showToolbar?: boolean
  title?: ReactNode
}

type CodeBlockContextType = {
  code: string
}

const CodeBlockContext = createContext<CodeBlockContextType>({
  code: '',
})

const lineNumberTransformer: ShikiTransformer = {
  name: 'line-numbers',
  line(node, line: number) {
    node.children.unshift({
      type: 'element',
      tagName: 'span',
      properties: {
        className: [
          'inline-block',
          'min-w-10',
          'mr-4',
          'text-right',
          'select-none',
          'text-muted-foreground',
        ],
      },
      children: [{ type: 'text', value: String(line) }],
    })
  },
}

export async function highlightCode(
  code: string,
  language: BundledLanguage | string,
  showLineNumbers = false
) {
  const transformers: ShikiTransformer[] = showLineNumbers
    ? [lineNumberTransformer]
    : []

  try {
    return await codeToHtml(code, {
      lang: language as BundledLanguage,
      themes: {
        light: 'one-light',
        dark: 'one-dark-pro',
      },
      transformers,
    })
  } catch {
    return codeToHtml(code, {
      lang: 'plaintext',
      themes: {
        light: 'one-light',
        dark: 'one-dark-pro',
      },
      transformers,
    })
  }
}

export const CodeBlock = ({
  collapsedLines,
  code,
  defaultCollapsed = false,
  language,
  maxExpandedLines,
  showLineNumbers = false,
  showToolbar = false,
  title,
  className,
  children,
  style,
  ...props
}: CodeBlockProps) => {
  const [html, setHtml] = useState<string>('')
  const [isCollapsed, setIsCollapsed] = useState(defaultCollapsed)
  const lineCount = code.split('\n').length
  const canToggleCollapse =
    typeof collapsedLines === 'number' &&
    collapsedLines > 0 &&
    lineCount > collapsedLines
  const activeLineLimit =
    canToggleCollapse && isCollapsed ? collapsedLines : maxExpandedLines
  const codeStyle =
    typeof activeLineLimit === 'number' && activeLineLimit > 0
      ? { maxHeight: `${activeLineLimit * 1.5 + 2}rem` }
      : undefined

  useEffect(() => {
    let cancelled = false
    highlightCode(code, language, showLineNumbers).then((next) => {
      if (!cancelled) {
        setHtml(next)
      }
    })
    return () => {
      cancelled = true
    }
  }, [code, language, showLineNumbers])

  useEffect(() => {
    setIsCollapsed(defaultCollapsed)
  }, [code, defaultCollapsed])

  return (
    <CodeBlockContext.Provider value={{ code }}>
      <div
        className={cn(
          'group bg-background text-foreground relative w-full overflow-hidden rounded-md border',
          className
        )}
        style={style}
        {...props}
      >
        {title && (
          <div className='bg-muted/30 border-border text-muted-foreground border-b px-3 py-2 text-xs font-medium'>
            {title}
          </div>
        )}
        <div className='relative'>
          <div
            className='[&>pre]:bg-background! [&>pre]:text-foreground! overflow-auto [&_code]:font-mono [&_code]:text-sm [&>pre]:m-0 [&>pre]:p-4 [&>pre]:text-sm'
            // biome-ignore lint/security/noDangerouslySetInnerHtml: "this is needed."
            dangerouslySetInnerHTML={{ __html: html }}
            style={codeStyle}
          />
          {(children || (showToolbar && canToggleCollapse)) && (
            <div className='absolute top-2 right-2 flex items-center gap-2'>
              {showToolbar && canToggleCollapse && (
                <Button
                  aria-label={
                    isCollapsed ? 'Expand code block' : 'Collapse code block'
                  }
                  onClick={() => setIsCollapsed((value) => !value)}
                  size='icon'
                  type='button'
                  variant='ghost'
                >
                  {isCollapsed ? (
                    <ChevronDownIcon size={14} />
                  ) : (
                    <ChevronUpIcon size={14} />
                  )}
                </Button>
              )}
              {children}
            </div>
          )}
        </div>
      </div>
    </CodeBlockContext.Provider>
  )
}

export type CodeBlockEditorProps = Omit<
  ComponentProps<'textarea'>,
  'className' | 'onChange' | 'title' | 'value'
> & {
  actions?: ReactNode
  ariaLabel?: string
  className?: string
  language?: string
  onChange?: (value: string) => void
  title?: ReactNode
  value: string
}

export const CodeBlockEditor = ({
  actions,
  ariaLabel,
  className,
  language = 'text',
  onChange,
  title,
  value,
  ...props
}: CodeBlockEditorProps) => (
  <div
    className={cn(
      'bg-background text-foreground w-full overflow-hidden rounded-md border',
      className
    )}
  >
    <div className='bg-muted/30 border-border flex min-h-10 items-center justify-between gap-3 border-b px-3 py-2'>
      <div className='text-muted-foreground min-w-0 text-xs font-medium tracking-normal'>
        {title ?? language}
      </div>
      {actions && (
        <div className='flex shrink-0 items-center gap-1'>{actions}</div>
      )}
    </div>
    <textarea
      {...props}
      aria-label={ariaLabel}
      className='bg-background text-foreground min-h-32 w-full resize-y border-0 p-4 font-mono text-sm leading-6 outline-none focus-visible:ring-0'
      onChange={(event) => onChange?.(event.target.value)}
      spellCheck={props.spellCheck ?? false}
      value={value}
    />
  </div>
)

export type CodeBlockCopyButtonProps = ComponentProps<typeof Button> & {
  onCopy?: () => void
  onError?: (error: Error) => void
  timeout?: number
}

export const CodeBlockCopyButton = ({
  onCopy,
  onError,
  timeout = 2000,
  children,
  className,
  ...props
}: CodeBlockCopyButtonProps) => {
  const [isCopied, setIsCopied] = useState(false)
  const { code } = useContext(CodeBlockContext)

  const copyToClipboard = async () => {
    if (typeof window === 'undefined' || !navigator?.clipboard?.writeText) {
      onError?.(new Error('Clipboard API not available'))
      return
    }

    try {
      await navigator.clipboard.writeText(code)
      setIsCopied(true)
      onCopy?.()
      setTimeout(() => setIsCopied(false), timeout)
    } catch (error) {
      onError?.(error as Error)
    }
  }

  const Icon = isCopied ? CheckIcon : CopyIcon

  return (
    <Button
      className={cn('shrink-0', className)}
      onClick={copyToClipboard}
      size='icon'
      variant='ghost'
      {...props}
    >
      {children ?? <Icon size={14} />}
    </Button>
  )
}
