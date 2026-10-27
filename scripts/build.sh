#!/bin/sh
# 构建单平台动态库。参数：<os> <arch>，例如 linux amd64。
set -eu
cd "$(dirname "$0")/.."

os_name="${1:-$(go env GOOS)}"
arch_name="${2:-$(go env GOARCH)}"

case "${os_name}_${arch_name}" in
  darwin_amd64|darwin_arm64|linux_amd64|linux_arm64|windows_amd64) ;;
  *) printf '不支持的平台: %s_%s\n' "$os_name" "$arch_name" >&2; exit 1 ;;
esac

extension=so
ldflags="-s -w"
case "$os_name" in
  darwin)
    extension=dylib
    if command -v xcrun >/dev/null 2>&1; then
      SDKROOT="$(xcrun --sdk macosx --show-sdk-path)"
      CC="$(xcrun --find clang)"
      MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-13.0}"
      export SDKROOT CC MACOSX_DEPLOYMENT_TARGET
    fi
    ;;
  linux)
    extension=so
    ldflags="$ldflags -extldflags=-Wl,--strip-debug"
    # 设置了 ZIG 就用 zig 交叉编译到 glibc 2.17，兼容老系统
    if [ -n "${ZIG:-}" ]; then
      case "$arch_name" in
        amd64) zig_target=x86_64-linux-gnu.2.17 ;;
        arm64) zig_target=aarch64-linux-gnu.2.17 ;;
      esac
      CC="$ZIG cc -target $zig_target"
      export CC
    elif [ "$(go env GOOS)_$(go env GOARCH)" != "${os_name}_${arch_name}" ]; then
      : "${CC:?跨平台编译请设置 CC，或设置 ZIG}"
    fi
    ;;
  windows)
    extension=dll
    # 静态链接，避免依赖 MinGW 运行时 DLL
    ldflags="$ldflags -extldflags=-static"
    if [ "$(go env GOOS)" != windows ]; then
      CC="${CC:-x86_64-w64-mingw32-gcc}"
      export CC
    fi
    ;;
esac

out_dir="dist/${os_name}_${arch_name}"
mkdir -p "$out_dir"

CGO_ENABLED=1 GOOS="$os_name" GOARCH="$arch_name" \
  go build -trimpath -ldflags="$ldflags" -buildmode=c-shared \
  -o "$out_dir/window-anchor.$extension" .

printf '%s\n' "$out_dir/window-anchor.$extension"
