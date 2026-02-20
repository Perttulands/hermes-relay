#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="relay.service"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOURCE_SERVICE_FILE="${SCRIPT_DIR}/deployment/${SERVICE_NAME}"
USER_SYSTEMD_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
DEST_SERVICE_FILE="${USER_SYSTEMD_DIR}/${SERVICE_NAME}"

if [[ ! -f "${SOURCE_SERVICE_FILE}" ]]; then
  echo "missing service file: ${SOURCE_SERVICE_FILE}" >&2
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemctl is required" >&2
  exit 1
fi

if ! command -v relay >/dev/null 2>&1; then
  echo "relay binary not found in PATH" >&2
  exit 1
fi

mkdir -p "${USER_SYSTEMD_DIR}"
install -m 644 "${SOURCE_SERVICE_FILE}" "${DEST_SERVICE_FILE}"

systemctl --user daemon-reload
systemctl --user enable --now "${SERVICE_NAME}"

echo "installed and started ${SERVICE_NAME}"
echo "status: systemctl --user status ${SERVICE_NAME}"
