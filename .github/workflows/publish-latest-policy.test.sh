#!/bin/bash
# Replays the publish-latest decision embedded in docker-build.yml.
set -euo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
file="$root/docker-build.yml"
block="$(sed -n '/# publish-latest-policy:begin/,/# publish-latest-policy:end/p' "$file")"
if [ -z "$block" ]; then
  echo "missing publish-latest policy in docker-build.yml" >&2
  exit 1
fi

policy() {
  local REF_NAME="$1"
  local PUBLISH_LATEST=""
  eval "$block"
  printf '%s' "$PUBLISH_LATEST"
}

[ "$(policy main-8910ca098)" = false ]
[ "$(policy v1.2.3)" = true ]
[ "$(policy v1.0.0-rc.40)" = true ]
[ "$(policy v1.0.0-alpha.1)" = true ]
echo POLICY_OK
