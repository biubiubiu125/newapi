import { describe, expect, test } from 'vitest'

import { buildTagEditRequest } from '../tag-edit'

describe('tag edit request', () => {
  test('dissolves the tag and clears every loaded model', () => {
    const result = buildTagEditRequest({
      currentTag: 'batch',
      newTag: '   ',
      modelMapping: '',
      selectedModels: [],
      initialModels: ['gpt-4', 'gpt-3.5'],
      modelsReady: true,
      selectedGroups: [],
      initialGroups: [],
      groupsReady: false,
    })

    expect(result.changed).toBe(true)
    expect(result.params).toEqual({
      tag: 'batch',
      new_tag: '',
      models: '',
    })
  })

  test('leaves models and groups unchanged when the form still matches the loaded values', () => {
    const result = buildTagEditRequest({
      currentTag: 'batch',
      newTag: 'batch',
      modelMapping: '',
      selectedModels: ['gpt-4'],
      initialModels: ['gpt-4'],
      modelsReady: true,
      selectedGroups: ['default'],
      initialGroups: ['default'],
      groupsReady: true,
    })

    expect(result.changed).toBe(false)
    expect(result.params).toEqual({ tag: 'batch' })
  })

  test('does not clear models before the current list has loaded', () => {
    const result = buildTagEditRequest({
      currentTag: 'batch',
      newTag: 'next',
      modelMapping: '',
      selectedModels: [],
      initialModels: [],
      modelsReady: false,
      selectedGroups: [],
      initialGroups: ['default'],
      groupsReady: false,
    })

    expect(result.params).toEqual({
      tag: 'batch',
      new_tag: 'next',
    })
  })

  test('clears groups only after the current groups have loaded', () => {
    const result = buildTagEditRequest({
      currentTag: 'batch',
      newTag: 'batch',
      modelMapping: '',
      selectedModels: ['gpt-4'],
      initialModels: ['gpt-4'],
      modelsReady: true,
      selectedGroups: [],
      initialGroups: ['default', 'vip'],
      groupsReady: true,
    })

    expect(result.changed).toBe(true)
    expect(result.params).toEqual({
      tag: 'batch',
      groups: '',
    })
  })
})
