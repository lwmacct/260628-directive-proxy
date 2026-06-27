#!/usr/bin/env bash
set -euo pipefail

CI_BIN_DIR="${1:-.cache/ci-bin}"

if [[ "${CI_BIN_DIR}" != /* ]]; then
  CI_BIN_DIR="${GITHUB_WORKSPACE:-$(pwd)}/${CI_BIN_DIR}"
fi

UPX_VERSION="5.1.1"
UPX_AMD64_SHA256="1ff660454227861e00772f743f66b900072116b9dc24f6ee28b97cce88a7828a"
UPX_ARCHIVE="upx-${UPX_VERSION}-amd64_linux.tar.xz"
UPX_URL="https://github.com/upx/upx/releases/download/v${UPX_VERSION}/${UPX_ARCHIVE}"

mkdir -p "${CI_BIN_DIR}"

install_upx() {
  local upx_bin="${CI_BIN_DIR}/upx"

  if [[ -x "${upx_bin}" ]]; then
    "${upx_bin}" --version
    return
  fi

  local tmp_dir
  tmp_dir="$(mktemp -d "${RUNNER_TEMP:-/tmp}/upx.XXXXXX")"
  trap 'rm -rf "${tmp_dir}"' RETURN

  curl -fsSL -o "${tmp_dir}/${UPX_ARCHIVE}" "${UPX_URL}"
  echo "${UPX_AMD64_SHA256}  ${tmp_dir}/${UPX_ARCHIVE}" | sha256sum -c -
  tar -C "${tmp_dir}" -xf "${tmp_dir}/${UPX_ARCHIVE}"
  install -m 0755 "${tmp_dir}/upx-${UPX_VERSION}-amd64_linux/upx" "${upx_bin}"
  "${upx_bin}" --version
}

install_upx
