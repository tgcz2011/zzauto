// Package installer 负责 zzauto 的自卸载与自升级。
//
// 升级走 GitHub releases 直链（不调用 gh api，避免频率限制），通过
// /releases/latest 的 302 重定向获取最新版本号，下载对应平台压缩包并
// sha256 校验后原子替换当前二进制。卸载仅移除二进制与配置文件，保留
// workspace/projects 项目数据。
package installer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// 仓库坐标与下载基地址。
const (
	repoOwner   = "tgcz2011"
	repoName    = "zzauto"
	binaryName  = "zzauto"
	releasesURL = "https://github.com/" + repoOwner + "/" + repoName + "/releases"
)

// releasesLatestURL 指向最新 release 页面（302 重定向到具体 tag）。
// 设为包变量便于测试覆写。
var releasesLatestURL = releasesURL + "/latest"

// CurrentVersion 当前二进制版本号，由 main 包覆写。
var CurrentVersion = "v0.1.0"

// ErrUnsupportedPlatform 表示当前平台无对应预编译产物。
type ErrUnsupportedPlatform struct{ goos, goarch string }

func (e ErrUnsupportedPlatform) Error() string {
	return fmt.Sprintf("不支持的平台: %s/%s", e.goos, e.goarch)
}

// assetName 根据平台返回 release 资产文件名（含扩展名）。
//
//	darwin/amd64  -> zzauto-darwin-amd64.tar.gz
//	linux/arm64   -> zzauto-linux-arm64.tar.gz
//	windows/amd64 -> zzauto-windows-amd64.zip
func assetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin", "linux":
		// 仅支持 amd64/arm64
		if goarch != "amd64" && goarch != "arm64" {
			return "", ErrUnsupportedPlatform{goos, goarch}
		}
		return fmt.Sprintf("%s-%s-%s.tar.gz", binaryName, goos, goarch), nil
	case "windows":
		if goarch != "amd64" {
			return "", ErrUnsupportedPlatform{goos, goarch}
		}
		return fmt.Sprintf("%s-%s-%s.zip", binaryName, goos, goarch), nil
	default:
		return "", ErrUnsupportedPlatform{goos, goarch}
	}
}

// tarballURL 构造指定版本的资产下载直链。
//
// version 为 "latest" 或具体版本号（如 "v0.1.0"）。
// 返回的 URL 未附加镜像前缀。
func tarballURL(version, goos, goarch string) (string, error) {
	name, err := assetName(goos, goarch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/download/%s", releasesURL, version, name), nil
}

// withMirror 在 URL 前附加镜像前缀（由 GITHUB_MIRROR 环境变量控制）。
//
// 例如 GITHUB_MIRROR=https://ghproxy.com/ 时，
// https://github.com/... 会被改写为 https://ghproxy.com/https://github.com/...
func withMirror(rawURL string) string {
	if m := os.Getenv("GITHUB_MIRROR"); m != "" {
		return m + rawURL
	}
	return rawURL
}

// parseVersionFromLocation 从重定向 Location 头解析版本号。
//
// 期望形如 https://github.com/tgcz2011/zzauto/releases/tag/v0.1.0 的 URL，
// 提取末尾的 tag 名作为版本号。
func parseVersionFromLocation(loc string) (string, error) {
	if loc == "" {
		return "", fmt.Errorf("重定向缺少 Location 头")
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("解析重定向 URL 失败: %w", err)
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	// 期望路径形如 /tgcz2011/zzauto/releases/tag/<version>
	if len(parts) < 2 || parts[len(parts)-2] != "tag" {
		return "", fmt.Errorf("无法从重定向 URL 解析版本号: %s", loc)
	}
	return parts[len(parts)-1], nil
}

// newNoRedirectClient 创建不自动跟随重定向的 HTTP 客户端。
//
// 用于捕获 /releases/latest 的 302 响应以提取版本号。
func newNoRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// newHTTPClient 创建跟随重定向的常规 HTTP 客户端（用于下载）。
func newHTTPClient() *http.Client {
	return &http.Client{}
}

// fetchLatestVersion 通过 /releases/latest 的 302 重定向获取最新版本号。
//
// 不消耗 GitHub API 限额。client 应为不跟随重定向的客户端。
func fetchLatestVersion(client *http.Client, latestURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "zzauto-installer/"+CurrentVersion)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// 接受 301/302/303/307/308 重定向
	if resp.StatusCode < 301 || resp.StatusCode > 308 {
		return "", fmt.Errorf("期望重定向响应，收到状态码 %d", resp.StatusCode)
	}
	return parseVersionFromLocation(resp.Header.Get("Location"))
}

// UpgradeCheck 检查是否有新版本，返回最新版本号。
//
// best-effort：失败时返回空字符串与错误，调用方可忽略错误友好提示。
func UpgradeCheck() (string, error) {
	return fetchLatestVersion(newNoRedirectClient(), withMirror(releasesLatestURL))
}

// computeFileSHA256 计算指定文件的 sha256 摘要（十六进制小写）。
func computeFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// parseSHA256File 解析 .sha256 文件内容，返回摘要（小写十六进制）。
//
// 兼容 "<hash>  <filename>" 与纯 "<hash>" 两种格式。
func parseSHA256File(data []byte) string {
	// 取首个空白分隔的字段
	fields := strings.Fields(strings.TrimSpace(string(data)))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

// downloadFile 下载 URL 到目标路径，返回字节数。
func downloadFile(client *http.Client, rawURL, destPath string) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "zzauto-installer/"+CurrentVersion)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("下载失败: %s 返回状态码 %d", rawURL, resp.StatusCode)
	}
	out, err := os.Create(destPath)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	return io.Copy(out, resp.Body)
}

// extractBinaryFromTarGz 从 tar.gz 中提取 zzauto 二进制到 destPath。
func extractBinaryFromTarGz(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("解压 gzip 失败: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取 tar 失败: %w", err)
		}
		// 仅处理普通文件，且名称为 zzauto（忽略路径前缀）
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base != binaryName && !strings.HasPrefix(base, binaryName+".") {
			// 跳过非二进制文件
			continue
		}
		if base != binaryName {
			continue
		}
		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
	return fmt.Errorf("压缩包中未找到 %s 二进制", binaryName)
}

// extractBinaryFromZip 从 zip 中提取 zzauto(.exe) 二进制到 destPath。
func extractBinaryFromZip(archivePath, destPath string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer zr.Close()
	target := binaryName + ".exe"
	for _, file := range zr.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(file.Name) != target && filepath.Base(file.Name) != binaryName {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		return out.Close()
	}
	return fmt.Errorf("压缩包中未找到 %s 二进制", target)
}

// extractBinary 根据平台从压缩包中提取二进制到 destPath。
func extractBinary(archivePath, goos, destPath string) error {
	if goos == "windows" {
		return extractBinaryFromZip(archivePath, destPath)
	}
	return extractBinaryFromTarGz(archivePath, destPath)
}

// currentBinaryPath 返回当前正在运行的二进制路径。
func currentBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取当前二进制路径失败: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil // 解析失败时退回原始路径
	}
	return resolved, nil
}

// configPaths 返回需要清理的配置文件/目录路径（不含 workspace）。
func configPaths() []string {
	var paths []string
	// 当前目录下的 zzauto.yaml
	if cfg, err := filepath.Abs("zzauto.yaml"); err == nil {
		paths = append(paths, cfg)
	}
	// 用户主目录下的 ~/.zzauto
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".zzauto"))
	}
	return paths
}

// pathExists 判断路径是否存在。
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Uninstall 移除二进制与配置（保留 workspace/projects 项目数据）。
//
// 步骤：
//  1. 删除当前二进制（os.Executable 获取路径）
//  2. 删除配置：./zzauto.yaml 与 ~/.zzauto
//  3. 打印删除了哪些文件
func Uninstall() error {
	var removed []string
	var errs []error

	// 1. 删除二进制
	if exe, err := currentBinaryPath(); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 获取二进制路径失败: %v\n", err)
		errs = append(errs, err)
	} else if pathExists(exe) {
		if err := os.Remove(exe); err != nil {
			fmt.Fprintf(os.Stderr, "警告: 删除二进制 %s 失败: %v\n", exe, err)
			errs = append(errs, err)
		} else {
			removed = append(removed, exe)
		}
	} else {
		fmt.Fprintf(os.Stderr, "提示: 二进制 %s 不存在（可能通过 go run 运行）\n", exe)
	}

	// 2. 删除配置
	for _, p := range configPaths() {
		if !pathExists(p) {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if err := os.RemoveAll(p); err != nil {
				fmt.Fprintf(os.Stderr, "警告: 删除目录 %s 失败: %v\n", p, err)
				errs = append(errs, err)
			} else {
				removed = append(removed, p+"/")
			}
		} else {
			if err := os.Remove(p); err != nil {
				fmt.Fprintf(os.Stderr, "警告: 删除文件 %s 失败: %v\n", p, err)
				errs = append(errs, err)
			} else {
				removed = append(removed, p)
			}
		}
	}

	// 3. 打印结果
	fmt.Println("已删除以下文件/目录（已保留 workspace/projects 项目数据）:")
	if len(removed) == 0 {
		fmt.Println("  （无）")
	}
	for _, p := range removed {
		fmt.Printf("  - %s\n", p)
	}

	if len(errs) > 0 {
		return fmt.Errorf("卸载过程中出现 %d 个错误", len(errs))
	}
	return nil
}

// Upgrade 从 GitHub releases 直链下载最新版并替换当前二进制。
//
// 不调用 gh api：通过 /releases/latest 的 302 重定向获取版本号，
// 下载对应平台压缩包，sha256 校验后原子替换当前二进制。
func Upgrade() error {
	fromVersion := CurrentVersion
	fmt.Printf("当前版本: %s\n", fromVersion)

	// 1. 获取最新版本号
	latest, err := UpgradeCheck()
	if err != nil {
		return fmt.Errorf("检查最新版本失败: %w\n提示: 请检查网络连接，或手动前往 %s 下载", err, releasesURL)
	}
	fmt.Printf("最新版本: %s\n", latest)

	if latest == fromVersion {
		fmt.Println("已是最新版本，无需升级。")
		return nil
	}

	// 2. 构造下载 URL
	asset, err := assetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return fmt.Errorf("当前平台无预编译产物: %w", err)
	}
	// 使用 latest/download 直链下载具体版本（已通过重定向确认存在）
	dlURL, err := tarballURL("latest", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	dlURL = withMirror(dlURL)
	shaURL := withMirror(dlURL + ".sha256")

	// 3. 下载压缩包到临时文件
	tmpDir, err := os.MkdirTemp("", "zzauto-upgrade-")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, asset)
	fmt.Printf("下载: %s\n", dlURL)
	client := newHTTPClient()
	if _, err := downloadFile(client, dlURL, archivePath); err != nil {
		return fmt.Errorf("下载压缩包失败: %w", err)
	}

	// 4. 下载并校验 sha256
	wantSum, err := fetchSHA256(client, shaURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: 获取 sha256 校验文件失败: %v（跳过校验）\n", err)
	} else {
		gotSum, err := computeFileSHA256(archivePath)
		if err != nil {
			return fmt.Errorf("计算下载文件 sha256 失败: %w", err)
		}
		if gotSum != wantSum {
			return fmt.Errorf("sha256 校验失败: 期望 %s 实际 %s", wantSum, gotSum)
		}
		fmt.Printf("sha256 校验通过: %s\n", wantSum)
	}

	// 5. 从压缩包提取二进制到临时文件
	exePath, err := currentBinaryPath()
	if err != nil {
		return err
	}
	// 提取到与目标二进制同目录的临时文件，确保 os.Rename 跨文件系统可用
	tmpBin := filepath.Join(filepath.Dir(exePath), ".zzauto.new")
	if err := extractBinary(archivePath, runtime.GOOS, tmpBin); err != nil {
		return fmt.Errorf("解压二进制失败: %w", err)
	}
	// 确保可执行权限
	if err := os.Chmod(tmpBin, 0o755); err != nil {
		os.Remove(tmpBin)
		return fmt.Errorf("设置可执行权限失败: %w", err)
	}

	// 6. 备份旧二进制并原子替换
	backup := exePath + ".bak"
	os.Remove(backup) // 清理可能残留的旧备份
	if pathExists(exePath) {
		if err := os.Rename(exePath, backup); err != nil {
			os.Remove(tmpBin)
			return fmt.Errorf("备份旧二进制失败: %w", err)
		}
	}
	if err := os.Rename(tmpBin, exePath); err != nil {
		// 替换失败，尝试回滚
		if pathExists(backup) {
			os.Rename(backup, exePath)
		}
		os.Remove(tmpBin)
		return fmt.Errorf("替换二进制失败: %w", err)
	}
	os.Remove(backup)

	fmt.Printf("升级完成: %s -> %s\n", fromVersion, latest)
	return nil
}

// fetchSHA256 下载 .sha256 文件并返回其中记录的摘要。
func fetchSHA256(client *http.Client, shaURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, shaURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "zzauto-installer/"+CurrentVersion)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sha256 文件返回状态码 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	sum := parseSHA256File(data)
	if sum == "" {
		return "", fmt.Errorf("sha256 文件内容为空")
	}
	return sum, nil
}
