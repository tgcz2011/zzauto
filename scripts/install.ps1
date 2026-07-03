# zzauto 一键安装脚本（Windows PowerShell）
#
# 用法：
#   irm https://github.com/tgcz2011/zzauto/raw/main/scripts/install.ps1 | iex
#
# 可选参数（通过管道传递变量较复杂，推荐用环境变量或直接下载脚本运行）：
#   $InstallVersion = "v0.x.x"   安装指定版本（默认 latest）
#   $InstallBin     = "C:\path\zzauto.exe"  指定安装路径
#   $Force          = $true      覆盖已存在的二进制
#   $env:GITHUB_MIRROR = "https://ghproxy.com/"  GitHub 下载镜像前缀（中国大陆加速）
#
# 设计要点：
#   - 不调用 gh api，走 GitHub releases 直链，绕开频率限制
#   - 探测 windows-amd64，下载 zip，sha256 校验
#   - 装到 $env:USERPROFILE\bin

# 严格模式，捕获未定义变量
$ErrorActionPreference = "Stop"

# ============ 配置 ============
$RepoOwner = "tgcz2011"
$RepoName = "zzauto"
$BinaryName = "zzauto"
$GitHubBase = "https://github.com/$RepoOwner/$RepoName"

# 参数默认值（允许通过会话变量覆盖）
if (-not (Test-Path Variable:InstallVersion)) { $InstallVersion = "latest" }
if (-not (Test-Path Variable:InstallBin))     { $InstallBin = "" }
if (-not (Test-Path Variable:Force))          { $Force = $false }

# ============ 工具函数 ============
function Write-Info([string]$msg) { Write-Host "[INFO] $msg" -ForegroundColor Blue }
function Write-Warn([string]$msg) { Write-Host "[WARN] $msg" -ForegroundColor Yellow }
function Write-Err([string]$msg)  { Write-Host "[ERR]  $msg" -ForegroundColor Red }

function Die([string]$msg) {
    Write-Err $msg
    exit 1
}

# ============ 平台探测 ============
# zzauto 仅提供 windows-amd64 预编译产物
function Test-Platform {
    $os = [System.Environment]::OSVersion.Platform
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($arch -notmatch "AMD64|ARM64") {
        # 实际仅支持 amd64，ARM64 暂无产物但给出提示
        Die "不支持的平台架构: $arch（仅支持 amd64）"
    }
    if ($arch -match "ARM64") {
        Die "暂不支持 windows-arm64，请使用 amd64 平台"
    }
    Write-Info "检测到平台: windows-amd64"
}

# ============ 镜像前缀 ============
# 返回带镜像前缀的 URL
function Get-MirroredUrl([string]$url) {
    $mirror = $env:GITHUB_MIRROR
    if ($mirror) {
        return "${mirror}${url}"
    }
    return $url
}

# ============ 主流程 ============
function Main {
    # 平台检查
    Test-Platform

    # 构造资产名与下载 URL
    $archiveName = "$BinaryName-windows-amd64.zip"
    if ($InstallVersion -eq "latest") {
        $downloadPath = "latest/download"
    } else {
        $downloadPath = "$InstallVersion/download"
    }
    $rawArchiveUrl = "$GitHubBase/releases/$downloadPath/$archiveName"
    $rawShaUrl = "$rawArchiveUrl.sha256"

    $archiveUrl = Get-MirroredUrl $rawArchiveUrl
    $shaUrl = Get-MirroredUrl $rawShaUrl

    Write-Info "下载: $archiveUrl"
    if ($env:GITHUB_MIRROR) {
        Write-Info "使用镜像: $env:GITHUB_MIRROR"
    }

    # 选择安装路径：$env:USERPROFILE\bin
    if (-not $InstallBin) {
        $installDir = Join-Path $env:USERPROFILE "bin"
        if (-not (Test-Path $installDir)) {
            New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        }
        $InstallBin = Join-Path $installDir "$BinaryName.exe"
    } else {
        $installDir = Split-Path $InstallBin -Parent
        if (-not (Test-Path $installDir)) {
            New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        }
    }

    # 检查已存在
    if ((Test-Path $InstallBin) -and -not $Force) {
        Die "目标已存在: $InstallBin（设置 `$Force = `$true 覆盖）"
    }

    Write-Info "安装到: $InstallBin"

    # 下载到临时目录
    $tmpDir = Join-Path $env:TEMP "zzauto-install-$(Get-Random)"
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null
    try {
        $archivePath = Join-Path $tmpDir $archiveName

        # 下载压缩包
        try {
            Invoke-WebRequest -Uri $archiveUrl -OutFile $archivePath -UseBasicParsing
        } catch {
            Die "下载失败: $archiveUrl - $_"
        }

        # sha256 校验
        try {
            $shaFile = "$archivePath.sha256"
            Invoke-WebRequest -Uri $shaUrl -OutFile $shaFile -UseBasicParsing
            $wantSha = ((Get-Content $shaFile) -split '\s+')[0].ToLower()
            if ($wantSha) {
                $gotSha = (Get-FileHash $archivePath -Algorithm SHA256).Hash.ToLower()
                if ($gotSha -ne $wantSha) {
                    Die "sha256 校验失败: 期望 $wantSha 实际 $gotSha"
                }
                Write-Info "sha256 校验通过: $gotSha"
            } else {
                Write-Warn "sha256 校验文件为空，跳过校验"
            }
        } catch {
            Write-Warn "无法下载 sha256 校验文件，跳过校验: $shaUrl"
        }

        # 解压 zip 并安装
        $extractDir = Join-Path $tmpDir "extracted"
        Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force
        $binPath = Get-ChildItem -Path $extractDir -Recurse -Filter "$BinaryName.exe" | Select-Object -First 1
        if (-not $binPath) {
            $binPath = Get-ChildItem -Path $extractDir -Recurse -Filter $BinaryName | Select-Object -First 1
        }
        if (-not $binPath) {
            Die "压缩包中未找到 $BinaryName 二进制"
        }
        Copy-Item -Path $binPath.FullName -Destination $InstallBin -Force

        # 验证安装
        $installedVer = "unknown"
        try {
            $verOut = & $InstallBin version 2>$null
            if ($LASTEXITCODE -eq 0 -and $verOut) {
                $installedVer = $verOut.Trim()
            }
        } catch {}

        Write-Host ""
        Write-Info "安装成功！"
        Write-Info "  路径: $InstallBin"
        Write-Info "  版本: $installedVer"

        # PATH 提示
        $userPath = [System.Environment]::GetEnvironmentVariable("Path", "User")
        if ($userPath -notlike "*$installDir*") {
            Write-Host ""
            Write-Warn "目录 $installDir 不在用户 PATH 中，请添加："
            Write-Host "    [System.Environment]::SetEnvironmentVariable('Path', `"$installDir;`$([System.Environment]::GetEnvironmentVariable('Path', 'User'))`", 'User')"
        }

        Write-Host ""
        Write-Info "运行 $BinaryName --help 查看用法"
    }
    finally {
        if (Test-Path $tmpDir) {
            Remove-Item -Path $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}

# 执行主流程
Main
