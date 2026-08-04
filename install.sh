#!/usr/bin/env sh
# ATM installer — 从 GitHub Release 下载预编译二进制
#   curl -fsSL https://raw.githubusercontent.com/zane-byte-dev/atm/main/install.sh | sh
set -e

REPO="zane-byte-dev/atm"
BIN="atm"
INSTALL_DIR="${ATM_INSTALL_DIR:-/usr/local/bin}"

say() { printf '%s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }

# 平台检测
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) die "unsupported OS: $os（Windows 请从 Release 页手动下载）" ;;
esac
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) die "unsupported arch: $arch" ;;
esac

# 取最新版本 tag
say "==> 查询最新版本…"
tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | head -1 | cut -d'"' -f4)"
[ -n "$tag" ] || die "无法获取最新版本，请检查网络或仓库是否已发布 Release"
version="${tag#v}"
say "==> 最新版本: ${tag}"

# 下载并解压
asset="${BIN}_${version}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
checksums_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "==> 下载 ${asset}…"
curl -fsSL "$url" -o "${tmp}/${asset}" || die "下载失败: $url"
curl -fsSL "$checksums_url" -o "${tmp}/checksums.txt" || die "校验文件下载失败: $checksums_url"

expected_checksum="$(awk -v name="$asset" '$2 == name { print $1; exit }' "${tmp}/checksums.txt")"
[ -n "$expected_checksum" ] || die "checksums.txt 中找不到 ${asset}"

if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "${tmp}/${asset}" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  actual_checksum="$(shasum -a 256 "${tmp}/${asset}" | awk '{ print $1 }')"
elif command -v openssl >/dev/null 2>&1; then
  actual_checksum="$(openssl dgst -sha256 "${tmp}/${asset}" | awk '{ print $NF }')"
else
  die "缺少 SHA-256 校验工具（需要 sha256sum、shasum 或 openssl）"
fi

[ "$actual_checksum" = "$expected_checksum" ] || die "${asset} 的 SHA-256 校验失败"
say "==> SHA-256 校验通过"
tar -xzf "${tmp}/${asset}" -C "$tmp"

# 安装（必要时 sudo）
if [ -w "$INSTALL_DIR" ]; then
  mv "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}"
else
  say "==> ${INSTALL_DIR} 需要权限，使用 sudo…"
  sudo mv "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}"
fi
chmod +x "${INSTALL_DIR}/${BIN}" 2>/dev/null || sudo chmod +x "${INSTALL_DIR}/${BIN}"

say "==> 已安装到 ${INSTALL_DIR}/${BIN}"
"${INSTALL_DIR}/${BIN}" version
