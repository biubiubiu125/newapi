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

export type DeploymentConfigFormValues = {
  image_url?: string
  traffic_port?: number
  entrypoint?: string
  args?: string
  command?: string
  registry_username?: string
  registry_secret?: string
  env_json?: string
  secret_env_json?: string
}

export type DeploymentUpdatePayload = {
  image_url?: string
  traffic_port?: number
  entrypoint: string[]
  args: string[]
  env_variables: Record<string, string>
  command?: string
  registry_username?: string
  registry_secret?: string
  secret_env_variables?: Record<string, string>
}

function splitWords(value?: string) {
  return (value ?? '')
    .split(/\s+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function parseStringMap(input?: string) {
  if (!input || !input.trim()) return undefined
  const parsed = JSON.parse(input)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('JSON must be an object')
  }
  return Object.fromEntries(
    Object.entries(parsed as Record<string, unknown>).map(([key, value]) => [
      key,
      String(value),
    ])
  )
}

export function buildDeploymentUpdatePayload(
  values: DeploymentConfigFormValues
): DeploymentUpdatePayload {
  const payload: DeploymentUpdatePayload = {
    entrypoint: splitWords(values.entrypoint),
    args: splitWords(values.args),
    env_variables: parseStringMap(values.env_json) ?? {},
  }
  const imageURL = values.image_url?.trim()
  if (imageURL) payload.image_url = imageURL
  if (
    typeof values.traffic_port === 'number' &&
    Number.isFinite(values.traffic_port)
  ) {
    payload.traffic_port = values.traffic_port
  }
  const command = values.command?.trim()
  if (command) payload.command = command
  const registryUsername = values.registry_username?.trim()
  if (registryUsername) payload.registry_username = registryUsername
  const registrySecret = values.registry_secret?.trim()
  if (registrySecret) payload.registry_secret = registrySecret
  const secretEnv = parseStringMap(values.secret_env_json)
  if (secretEnv && Object.keys(secretEnv).length > 0) {
    payload.secret_env_variables = secretEnv
  }
  return payload
}

export function deploymentConfigFormValues(config: {
  image_url?: string
  traffic_port?: number
  entrypoint?: string[]
  args?: string[]
  env_variables?: Record<string, unknown>
}): DeploymentConfigFormValues {
  const env = config.env_variables ?? {}
  return {
    image_url: config.image_url ?? '',
    traffic_port: config.traffic_port,
    entrypoint: (config.entrypoint ?? []).join(' '),
    args: (config.args ?? []).join(' '),
    command: '',
    registry_username: '',
    registry_secret: '',
    env_json: Object.keys(env).length ? JSON.stringify(env, null, 2) : '',
    secret_env_json: '',
  }
}
