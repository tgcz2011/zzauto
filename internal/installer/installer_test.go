package installer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestAssetName 验证各平台的 release 资产文件名构造。
func TestAssetName(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
		wantErr            bool
	}{
		{"darwin", "amd64", "zzauto-darwin-amd64.tar.gz", false},
		{"darwin", "arm64", "zzauto-darwin-arm64.tar.gz", false},
		{"linux", "amd64", "zzauto-linux-amd64.tar.gz", false},
		{"linux", "arm64", "zzauto-linux-arm64.tar.gz", false},
		{"windows", "amd64", "zzauto-windows-amd64.zip", false},
		// 不支持的平台
		{"windows", "arm64", "", true},
		{"freebsd", "amd64", "", true},
		{"darwin", "386", "", true},
	}
	for _, c := range cases {
		got, err := assetName(c.goos, c.goarch)
		if c.wantErr {
			if err == nil {
				t.Errorf("assetName(%s,%s) 期望错误，实际得到 %q", c.goos, c.goarch, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("assetName(%s,%s) 意外错误: %v", c.goos, c.goarch, err)
			continue
		}
		if got != c.want {
			t.Errorf("assetName(%s,%s) = %q, 期望 %q", c.goos, c.goarch, got, c.want)
		}
	}
}

// TestTarballURL 验证下载直链构造。
func TestTarballURL(t *testing.T) {
	// latest 直链
	got, err := tarballURL("latest", "darwin", "arm64")
	if err != nil {
		t.Fatalf("tarballURL 意外错误: %v", err)
	}
	want := "https://github.com/tgcz2011/zzauto/releases/latest/download/zzauto-darwin-arm64.tar.gz"
	if got != want {
		t.Errorf("tarballURL(latest) = %q, 期望 %q", got, want)
	}

	// 指定版本直链
	got, err = tarballURL("v0.2.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("tarballURL 意外错误: %v", err)
	}
	want = "https://github.com/tgcz2011/zzauto/releases/v0.2.0/download/zzauto-linux-amd64.tar.gz"
	if got != want {
		t.Errorf("tarballURL(v0.2.0) = %q, 期望 %q", got, want)
	}

	// windows zip
	got, err = tarballURL("latest", "windows", "amd64")
	if err != nil {
		t.Fatalf("tarballURL 意外错误: %v", err)
	}
	want = "https://github.com/tgcz2011/zzauto/releases/latest/download/zzauto-windows-amd64.zip"
	if got != want {
		t.Errorf("tarballURL(windows) = %q, 期望 %q", got, want)
	}

	// 不支持平台应报错
	if _, err := tarballURL("latest", "freebsd", "amd64"); err == nil {
		t.Error("tarballURL(freebsd) 期望错误，实际无错误")
	}
}

// TestParseVersionFromLocation 验证从重定向 URL 解析版本号。
func TestParseVersionFromLocation(t *testing.T) {
	cases := []struct {
		loc     string
		want    string
		wantErr bool
	}{
		{"https://github.com/tgcz2011/zzauto/releases/tag/v0.1.0", "v0.1.0", false},
		{"https://github.com/tgcz2011/zzauto/releases/tag/v1.2.3-rc1/", "v1.2.3-rc1", false},
		{"", "", true},
		{"https://github.com/tgcz2011/zzauto/releases", "", true},
		{"https://example.com/foo/bar", "", true},
	}
	for _, c := range cases {
		got, err := parseVersionFromLocation(c.loc)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseVersionFromLocation(%q) 期望错误，实际得到 %q", c.loc, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseVersionFromLocation(%q) 意外错误: %v", c.loc, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseVersionFromLocation(%q) = %q, 期望 %q", c.loc, got, c.want)
		}
	}
}

// TestParseSHA256File 验证 sha256 文件内容解析。
func TestParseSHA256File(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"abc123  zzauto-darwin-arm64.tar.gz\n", "abc123"},
		{"ABC123  zzauto-darwin-arm64.tar.gz", "abc123"}, // 转小写
		{"deadbeef\n", "deadbeef"},
		{"  \n", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := parseSHA256File([]byte(c.input))
		if got != c.want {
			t.Errorf("parseSHA256File(%q) = %q, 期望 %q", c.input, got, c.want)
		}
	}
}

// TestFetchLatestVersion 用 httptest mock 验证通过 302 重定向获取版本号（不实际联网）。
func TestFetchLatestVersion(t *testing.T) {
	// 模拟 GitHub /releases/latest 的 302 重定向行为
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验请求路径
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			// 对于 tag 页面（重定向目标），返回 200
			if strings.Contains(r.URL.Path, "/releases/tag/") {
				w.WriteHeader(http.StatusOK)
				return
			}
			http.NotFound(w, r)
			return
		}
		// 返回 302 重定向到具体 tag 页面
		w.Header().Set("Location", "https://github.com/tgcz2011/zzauto/releases/tag/v0.9.9")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := newNoRedirectClient()
	got, err := fetchLatestVersion(client, srv.URL+"/releases/latest")
	if err != nil {
		t.Fatalf("fetchLatestVersion 意外错误: %v", err)
	}
	if got != "v0.9.9" {
		t.Errorf("fetchLatestVersion = %q, 期望 %q", got, "v0.9.9")
	}
}

// TestFetchLatestVersion_NoRedirect 验证非重定向响应报错。
func TestFetchLatestVersion_NoRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // 应为重定向，但返回 200
	}))
	defer srv.Close()

	client := newNoRedirectClient()
	_, err := fetchLatestVersion(client, srv.URL+"/releases/latest")
	if err == nil {
		t.Fatal("fetchLatestVersion 期望错误，实际无错误")
	}
	if !strings.Contains(err.Error(), "重定向") {
		t.Errorf("错误信息应包含\"重定向\"，实际: %v", err)
	}
}

// TestFetchLatestVersion_MissingLocation 验证重定向缺少 Location 头时报错。
func TestFetchLatestVersion_MissingLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 302 但不设 Location
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := newNoRedirectClient()
	_, err := fetchLatestVersion(client, srv.URL+"/releases/latest")
	if err == nil {
		t.Fatal("fetchLatestVersion 期望错误，实际无错误")
	}
}

// TestWithMirror 验证镜像前缀附加逻辑。
func TestWithMirror(t *testing.T) {
	const rawURL = "https://github.com/tgcz2011/zzauto/releases/latest/download/zzauto-darwin-arm64.tar.gz"

	// 未设置环境变量，原样返回
	os.Unsetenv("GITHUB_MIRROR")
	if got := withMirror(rawURL); got != rawURL {
		t.Errorf("无镜像时 withMirror = %q, 期望 %q", got, rawURL)
	}

	// 设置镜像前缀
	os.Setenv("GITHUB_MIRROR", "https://ghproxy.com/")
	defer os.Unsetenv("GITHUB_MIRROR")
	got := withMirror(rawURL)
	want := "https://ghproxy.com/" + rawURL
	if got != want {
		t.Errorf("有镜像时 withMirror = %q, 期望 %q", got, want)
	}
}

// TestUpgradeCheck_WithMockServer 通过覆写 releasesLatestURL 用 httptest 验证
// UpgradeCheck 的端到端 URL 构造与版本解析逻辑（不实际联网）。
func TestUpgradeCheck_WithMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://github.com/tgcz2011/zzauto/releases/tag/v1.0.0")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	// 备份并覆写包变量
	orig := releasesLatestURL
	releasesLatestURL = srv.URL + "/releases/latest"
	origMirror := os.Getenv("GITHUB_MIRROR")
	os.Unsetenv("GITHUB_MIRROR")
	defer func() {
		releasesLatestURL = orig
		os.Setenv("GITHUB_MIRROR", origMirror)
	}()

	got, err := UpgradeCheck()
	if err != nil {
		t.Fatalf("UpgradeCheck 意外错误: %v", err)
	}
	if got != "v1.0.0" {
		t.Errorf("UpgradeCheck = %q, 期望 %q", got, "v1.0.0")
	}
}

// TestUpgradeCheck_MirrorApplied 验证 UpgradeCheck 会附加镜像前缀。
func TestUpgradeCheck_MirrorApplied(t *testing.T) {
	var hitPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPath = r.Host + r.URL.Path
		w.Header().Set("Location", "https://github.com/tgcz2011/zzauto/releases/tag/v0.5.0")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	orig := releasesLatestURL
	releasesLatestURL = "https://github.com/tgcz2011/zzauto/releases/latest"
	os.Setenv("GITHUB_MIRROR", srv.URL+"/")
	defer func() {
		releasesLatestURL = orig
		os.Unsetenv("GITHUB_MIRROR")
	}()

	got, err := UpgradeCheck()
	if err != nil {
		t.Fatalf("UpgradeCheck 意外错误: %v", err)
	}
	if got != "v0.5.0" {
		t.Errorf("UpgradeCheck = %q, 期望 %q", got, "v0.5.0")
	}
	// 验证请求确实经过镜像前缀（即请求打到了 mock server）
	if hitPath == "" {
		t.Error("UpgradeCheck 似乎未通过镜像前缀访问 mock server")
	}
}
