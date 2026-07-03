#!/usr/bin/env sh
# zzauto 一键安装脚本（macOS / Linux）。
#
# 用法：
#   curl -fsSL https://github.com/tgcz2011/zzauto/raw/main/scripts/install.sh | sh
#
# 可选参数：
#   --version v0.x.x   安装指定版本（默认 latest）
#   --bin <path>        指定安装路径（默认 /usr/local/bin/zzauto，不可写则 ~/.local/bin/zzauto）
#   --force             覆盖已存在的二进制
#
# 环境变量：
#   GITHUB_MIRROR       GitHub 下载镜像前缀（中国大陆加速，如 https://ghproxy.com/）
#   https_proxy         HTTPS 代理地址
#
# 设计要点：
#   - 不调用 gh api，走 GitHub releases 直链，绕开频率限制
#   - curl 使用 --http1.1 避免 HTTP/2 framing 错误，--retry 3 应对网络波动
#   - sha256 校验：下载 .tar.gz.sha256 文件对比
set -euo pipefail

# ============ 配置 ============
REPO_OWNER="tgcz2011"
REPO_NAME="zzauto"
BINARY_NAME="zzauto"
GITHUB_BASE="https://github.com/${REPO_OWNER}/${REPO_NAME}"

# 默认参数
INSTALL_VERSION="latest"
INSTALL_BIN=""
FORCE=0

# ============ 工具函数 ============
# 输出信息到 stderr（不干扰管道）
info() {
    printf '\033[1;34m[INFO]\033[0m %s\n' "$*" >&2
}
warn() {
    printf '\033[1;33m[WARN]\033[0m %s\n' "$*" >&2
}
err() {
    printf '\033[1;31m[ERR]\033[0m %s\n' "$*" >&2
}
die() {
    err "$*"
    exit 1
}

# 判断命令是否存在
has() {
    command -v "$1" >/dev/null 2>&1
}

# ============ 参数解析 ============
parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --version)
                INSTALL_VERSION="${2:-}"
                [ -z "$INSTALL_VERSION" ] && die "--version 需要参数"
                shift 2
                ;;
            --version=*)
                INSTALL_VERSION="${1#--version=}"
                shift
                ;;
            --bin)
                INSTALL_BIN="${2:-}"
                [ -z "$INSTALL_BIN" ] && die "--bin 需要参数"
                shift 2
                ;;
            --bin=*)
                INSTALL_BIN="${1#--bin=}"
                shift
                ;;
            --force)
                FORCE=1
                shift
                ;;
            -h|--help)
                cat >&2 <<EOF
zzauto 安装脚本

用法: curl -fsSL .../install.sh | sh -s -- [选项]

选项:
  --version v0.x.x   安装指定版本（默认 latest）
  --bin <path>        指定安装路径
  --force             覆盖已存在的二进制
  -h, --help          显示帮助

环境变量:
  GITHUB_MIRROR       GitHub 下载镜像前缀
  https_proxy         HTTPS 代理地址
EOF
                exit 0
                ;;
            *)
                die "未知参数: $1（使用 -h 查看帮助）"
                ;;
        esac
    done
}

# ============ 平台探测 ============
# 探测 GOOS（darwin / linux）
detect_goos() {
    os="$(uname -s)"
    case "$os" in
        Darwin) echo "darwin" ;;
        Linux)  echo "linux" ;;
        *) die "不支持的操作系统: $os（仅支持 macOS / Linux）" ;;
    esac
}

# 探测 GOARCH（amd64 / arm64）
detect_goarch() {
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64) echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *) die "不支持的平台架构: $arch（仅支持 amd64 / arm64）" ;;
    esac
}

# ============ 镜像前缀 ============
# 返回带镜像前缀的 URL（GITHUB_MIRROR 设置时附加前缀）
mirror_url() {
    url="$1"
    if [ -n "${GITHUB_MIRROR:-}" ]; then
        printf '%s%s' "$GITHUB_MIRROR" "$url"
    else
        printf '%s' "$url"
    fi
}

# ============ 安装路径选择 ============
# 选择可写的安装目录
choose_install_dir() {
    # 优先 /usr/local/bin
    if [ -w "/usr/local/bin" ] 2>/dev/null; then
        echo "/usr/local/bin"
        return
    fi
    # 尝试 sudo 写 /usr/local/bin（仅当目录存在）
    if [ -d "/usr/local/bin" ] && has sudo && sudo -n true 2>/dev/null; then
        echo "/usr/local/bin"
        return
    fi
    # 回退到 ~/.local/bin
    home_bin="${HOME}/.local/bin"
    mkdir -p "$home_bin" 2>/dev/null || true
    echo "$home_bin"
}

# ============ 下载与校验 ============
# curl 公共参数：--http1.1 避免 HTTP/2 framing 错误，--retry 3 应对网络波动
CURL_OPTS="-fsSL --http1.1 --retry 3 --retry-delay 2"

# 下载文件到指定路径
download() {
    url="$1"
    dest="$2"
    # shellcheck disable=SC2086
    curl $CURL_OPTS -o "$dest" "$url" || die "下载失败: $url"
}

# 计算 sha256 摘要（兼容 macOS shasum 与 Linux sha256sum）
sha256_sum() {
    file="$1"
    if has shasum; then
        shasum -a 256 "$file" | awk '{print $1}'
    elif has sha256sum; then
        sha256sum "$file" | awk '{print $1}'
    else
        die "未找到 shasum 或 sha256sum 命令，无法校验"
    fi
}

# 校验 sha256：下载 <file>.sha256 并对比
verify_sha256() {
    archive="$1"
    sha_url="$2"

    # 下载校验文件（失败则警告并跳过）
    sha_file="${archive}.sha256"
    if ! curl $CURL_OPTS -o "$sha_file" "$sha_url" 2>/dev/null; then
        warn "无法下载 sha256 校验文件，跳过校验: $sha_url"
        return 0
    fi

    # 解析期望摘要（取首个字段，兼容 "<hash>  <filename>" 格式）
    want_sha="$(awk '{print $1}' "$sha_file" | tr '[:upper:]' '[:lower:]')"
    if [ -z "$want_sha" ]; then
        warn "sha256 校验文件为空，跳过校验"
        return 0
    fi

    # 计算实际摘要
    got_sha="$(sha256_sum "$archive")"
    if [ "$got_sha" != "$want_sha" ]; then
        die "sha256 校验失败: 期望 $want_sha 实际 $got_sha"
    fi
    info "sha256 校验通过: $got_sha"
}

# ============ 解压 ============
# 从 tar.gz 提取 zzauto 二进制到目标路径
extract_binary() {
    archive="$1"
    dest="$2"
    # tar.gz 包含 zzauto 二进制
    tmp_extract="$(mktemp -d)"
    tar -xzf "$archive" -C "$tmp_extract" || die "解压失败: $archive"
    # 查找二进制（可能在子目录，取名为 zzauto）
    bin_path=""
    for f in "$tmp_extract"/"$BINARY_NAME" "$tmp_extract"/*/"$BINARY_NAME"; do
        if [ -f "$f" ]; then
            bin_path="$f"
            break
        fi
    done
    if [ -z "$bin_path" ]; then
        rm -rf "$tmp_extract"
        die "压缩包中未找到 $BINARY_NAME 二进制"
    fi
    mv -f "$bin_path" "$dest" || die "移动二进制到 $dest 失败"
    rm -rf "$tmp_extract"
    chmod 0755 "$dest" || die "设置可执行权限失败"
}

# ============ 主流程 ============
main() {
    parse_args "$@"

    # 依赖检查
    has curl || die "未找到 curl 命令，请先安装 curl"
    has tar  || die "未找到 tar 命令，请先安装 tar"

    # 探测平台
    goos="$(detect_goos)"
    goarch="$(detect_goarch)"
    info "检测到平台: ${goos}/${goarch}"

    # 构造资产名与下载 URL
    archive_name="${BINARY_NAME}-${goos}-${goarch}.tar.gz"
    if [ "$INSTALL_VERSION" = "latest" ]; then
        download_path="latest/download"
    else
        download_path="${INSTALL_VERSION}/download"
    fi
    raw_archive_url="${GITHUB_BASE}/releases/${download_path}/${archive_name}"
    raw_sha_url="${raw_archive_url}.sha256"

    # 附加镜像前缀
    archive_url="$(mirror_url "$raw_archive_url")"
    sha_url="$(mirror_url "$raw_sha_url")"

    info "下载: $archive_url"
    [ -n "${GITHUB_MIRROR:-}" ] && info "使用镜像: $GITHUB_MIRROR"

    # 选择安装路径
    if [ -z "$INSTALL_BIN" ]; then
        install_dir="$(choose_install_dir)"
        INSTALL_BIN="${install_dir}/${BINARY_NAME}"
    else
        install_dir="$(dirname "$INSTALL_BIN")"
        mkdir -p "$install_dir" 2>/dev/null || die "无法创建安装目录: $install_dir"
    fi

    # 检查已存在
    if [ -e "$INSTALL_BIN" ] && [ "$FORCE" -ne 1 ]; then
        die "目标已存在: $INSTALL_BIN（使用 --force 覆盖）"
    fi

    info "安装到: $INSTALL_BIN"

    # 下载到临时目录
    tmp_dir="$(mktemp -d)"
    trap 'rm -rf "$tmp_dir"' EXIT

    archive_path="${tmp_dir}/${archive_name}"
    download "$archive_url" "$archive_path"

    # sha256 校验
    verify_sha256 "$archive_path" "$sha_url"

    # 解压并安装
    extract_binary "$archive_path" "$INSTALL_BIN"

    # 验证安装
    if "$INSTALL_BIN" version >/dev/null 2>&1; then
        installed_ver="$("$INSTALL_BIN" version 2>/dev/null || echo unknown)"
    else
        installed_ver="unknown"
    fi

    echo ""
    info "安装成功！"
    info "  路径: $INSTALL_BIN"
    info "  版本: $installed_ver"

    # PATH 提示
    case ":${PATH}:" in
        *":${install_dir}:"*) ;;
        *)
            echo ""
            warn "目录 $install_dir 不在 PATH 中，请添加以下内容到 shell 配置（~/.bashrc 或 ~/.zshrc）："
            echo "    export PATH=\"${install_dir}:\$PATH\""
            ;;
    esac

    echo ""
    info "运行 ${BINARY_NAME} --help 查看用法"
}

# 仅当直接执行（非 source）时运行 main
# 注意：curl | sh 场景下 $0 不是脚本文件，用此判断更稳妥
main "$@"
