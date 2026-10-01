import { describe, expect, test } from 'vitest'

import {
  buildDeploymentUpdatePayload,
  deploymentConfigFormValues,
} from '../deployment-update'

const blank = {
  image_url: 'example.io/app',
  traffic_port: 8080,
  entrypoint: '',
  args: '',
  command: '',
  registry_username: '',
  registry_secret: '',
  env_json: '   ',
  secret_env_json: '',
}

describe('deployment configuration update', () => {
  test('sends empty entrypoint, args, and env so they can be cleared', () => {
    expect(buildDeploymentUpdatePayload(blank)).toEqual({
      image_url: 'example.io/app',
      traffic_port: 8080,
      entrypoint: [],
      args: [],
      env_variables: {},
    })
  })

  test('sends a command only when one is entered and never sends blank secrets', () => {
    const payload = buildDeploymentUpdatePayload({
      ...blank,
      command: ' run ',
      registry_username: ' ',
      secret_env_json: '',
    })

    expect(payload.command).toBe('run')
    expect(payload).not.toHaveProperty('registry_username')
    expect(payload).not.toHaveProperty('registry_secret')
    expect(payload).not.toHaveProperty('secret_env_variables')
  })

  test('loads saved args back into the form', () => {
    expect(
      deploymentConfigFormValues({
        image_url: 'example.io/app',
        traffic_port: 80,
        entrypoint: ['bash', '-lc'],
        args: ['--foo', 'bar'],
        env_variables: { A: '1' },
      })
    ).toMatchObject({
      image_url: 'example.io/app',
      traffic_port: 80,
      entrypoint: 'bash -lc',
      args: '--foo bar',
    })
  })
})
