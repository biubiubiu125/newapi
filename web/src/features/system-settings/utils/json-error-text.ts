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
import { t } from 'i18next'

function jsonErrorPosition(error: Error, jsonString: string) {
  const positionMatch = error.message.match(/at position (\d+)/i)
  if (positionMatch) {
    const position = Number.parseInt(positionMatch[1], 10)
    const lines = jsonString.slice(0, position).split('\n')
    return {
      line: lines.length,
      column: (lines.at(-1) ?? '').length + 1,
      position,
    }
  }

  const lineColMatch = error.message.match(/at line (\d+) column (\d+)/i)
  if (lineColMatch) {
    return {
      line: Number.parseInt(lineColMatch[1], 10),
      column: Number.parseInt(lineColMatch[2], 10),
    }
  }

  return {}
}

function missingCommaLine(message: string, line: number | undefined) {
  const missing =
    message.includes("Expected ','") ||
    message.includes('Expected property name') ||
    message.includes('Unexpected string')
  if (!missing || !line || line <= 1) return undefined
  return line - 1
}

// consoleJsonErrorText reports where JSON failed without the browser's English parser sentence.
export function consoleJsonErrorText(error: unknown, jsonString: string) {
  if (!(error instanceof Error)) return t('Invalid JSON')

  const position = jsonErrorPosition(error, jsonString)
  if (position.line && position.column) {
    const located = t('Invalid JSON at line {{line}}, column {{column}}', {
      line: position.line,
      column: position.column,
    })
    const commaLine = missingCommaLine(error.message, position.line)
    if (!commaLine) return located
    return `${located} ${t('Check line {{line}} for a missing comma', {
      line: commaLine,
    })}`
  }

  if (position.position !== undefined) {
    return t('Invalid JSON at position {{position}}', {
      position: position.position,
    })
  }

  return t('Invalid JSON')
}
