package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, "proj-1")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	for _, sub := range []string{"", "agents", "reports"} {
		p := filepath.Join(w.Path(), sub)
		fi, err := os.Stat(p)
		if err != nil {
			t.Errorf("期望目录存在 %s: %v", p, err)
			continue
		}
		if !fi.IsDir() {
			t.Errorf("期望是目录 %s", p)
		}
	}
}

func TestWriteAndReadDoc(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, "proj-2")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	content := "# desire\n用户原始欲望\n"
	if err := w.WriteDoc(DocDesire, content); err != nil {
		t.Fatalf("WriteDoc 失败: %v", err)
	}
	got, err := w.ReadDoc(DocDesire)
	if err != nil {
		t.Fatalf("ReadDoc 失败: %v", err)
	}
	if got != content {
		t.Errorf("ReadDoc 内容不匹配:\n got=%q\nwant=%q", got, content)
	}
	// DocPath 指向项目目录下的文件
	if got, want := w.DocPath(DocDesire), filepath.Join(w.Path(), DocDesire); got != want {
		t.Errorf("DocPath=%s, want=%s", got, want)
	}
}

func TestDocRoundTrip(t *testing.T) {
	meta := DocMeta{
		Stage:     StageListener,
		Status:    StatusDone,
		UpdatedAt: time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
	}
	body := "正文内容\n第二行"
	rendered := RenderDoc(meta, body)

	gotMeta, gotBody := ParseDoc(rendered)
	if gotMeta.Stage != meta.Stage {
		t.Errorf("Stage 不匹配: got=%q want=%q", gotMeta.Stage, meta.Stage)
	}
	if gotMeta.Status != meta.Status {
		t.Errorf("Status 不匹配: got=%q want=%q", gotMeta.Status, meta.Status)
	}
	if !gotMeta.UpdatedAt.Equal(meta.UpdatedAt) {
		t.Errorf("UpdatedAt 不匹配: got=%v want=%v", gotMeta.UpdatedAt, meta.UpdatedAt)
	}
	if gotBody != body {
		t.Errorf("Body 不匹配:\n got=%q\nwant=%q", gotBody, body)
	}
}

func TestParseDocNoFrontmatter(t *testing.T) {
	raw := "纯正文，没有 frontmatter"
	meta, body := ParseDoc(raw)
	if meta.Stage != "" || meta.Status != "" {
		t.Errorf("无 frontmatter 时 meta 应为零值: %+v", meta)
	}
	if body != raw {
		t.Errorf("Body 不匹配: got=%q want=%q", body, raw)
	}
}

func TestGenerateProjectID(t *testing.T) {
	id := GenerateProjectID()
	if id == "" {
		t.Fatal("GenerateProjectID 返回空")
	}
	id2 := GenerateProjectID()
	if id == id2 {
		t.Errorf("两次生成的 projectID 不应相同: %s", id)
	}
}
