#!/usr/bin/env python3
"""把 dist/ 下五个平台的动态库打成插件商店格式的发布包。

产物：
  release/cpa-quota-window_<version>_<platform>.zip   每个包含一个动态库
  release/checksums.txt                            sha256 校验和
"""
import hashlib
import pathlib
import re
import sys
import zipfile

PLATFORMS = ["darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64", "windows_amd64"]
EXTENSIONS = {"darwin": "dylib", "linux": "so", "windows": "dll"}
LIBRARY = "cpa-quota-window"

root = pathlib.Path(__file__).resolve().parents[1]


def read_version() -> str:
    source = (root / "internal" / "wa" / "config.go").read_text(encoding="utf-8")
    match = re.search(r'const Version = "([^"]+)"', source)
    if not match:
        raise SystemExit("internal/wa/config.go 里找不到 Version")
    return match.group(1)


def main() -> int:
    version = read_version()
    output = root / "release"
    output.mkdir(parents=True, exist_ok=True)

    # 先整体校验，避免只打了一半的包
    libraries = {}
    for platform in PLATFORMS:
        os_name = platform.split("_")[0]
        library = root / "dist" / platform / f"{LIBRARY}.{EXTENSIONS[os_name]}"
        if not library.is_file():
            print(f"缺少构建产物: {library}", file=sys.stderr)
            return 1
        if library.stat().st_size < 256 * 1024:
            print(f"产物过小，可能不是有效动态库: {library}", file=sys.stderr)
            return 1
        libraries[platform] = library

    checksums = []
    for platform, library in libraries.items():
        archive = output / f"{LIBRARY}_{version}_{platform}.zip"
        with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as bundle:
            bundle.write(library, library.name)
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        checksums.append(f"{digest}  {archive.name}")
        print(f"{archive.name}  {digest}")

    (output / "checksums.txt").write_text("\n".join(checksums) + "\n", encoding="utf-8")
    print(f"\n版本 {version}，共 {len(checksums)} 个平台包，已写入 release/checksums.txt")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
