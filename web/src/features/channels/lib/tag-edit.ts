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
import type { TagOperationParams } from '../types'

export type TagEditRequestInput = {
  currentTag: string
  newTag: string
  modelMapping: string
  selectedModels: string[]
  initialModels: string[]
  modelsReady: boolean
  selectedGroups: string[]
  initialGroups: string[]
  groupsReady: boolean
}

function sameList(left: string[], right: string[]) {
  return (
    left.length === right.length &&
    left.every((item, index) => item === right[index])
  )
}

export function buildTagEditRequest(input: TagEditRequestInput): {
  changed: boolean
  params: TagOperationParams
} {
  const params: TagOperationParams = { tag: input.currentTag }
  const nextTag = input.newTag.trim()
  if (nextTag !== input.currentTag) {
    params.new_tag = nextTag
  }
  const modelMapping = input.modelMapping.trim()
  if (modelMapping) {
    params.model_mapping = modelMapping
  }
  if (
    input.modelsReady &&
    !sameList(input.selectedModels, input.initialModels)
  ) {
    params.models = input.selectedModels.join(',')
  }
  if (
    input.groupsReady &&
    !sameList(input.selectedGroups, input.initialGroups)
  ) {
    params.groups = input.selectedGroups.join(',')
  }
  return {
    changed: Object.keys(params).length > 1,
    params,
  }
}
