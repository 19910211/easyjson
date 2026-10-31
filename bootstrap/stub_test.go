package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteStubKeepsFromPool(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "vo_easyjson.go")
	old := "package vo\nfunc NestedFromPool() *Nested { return nil }\nfunc (m *Nested) Clone() *Nested { return nil }\n"
	if err := os.WriteFile(out, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	g := Generator{
		PkgName:      "vo",
		Types:        []string{"AdsResponse"},
		PoolStructs:  []string{"AdsResponse", "OnlyPool"},
		CloneStructs: []string{"AdsResponse"},
		OutName:      out,
	}
	if err := g.writeStub([]byte(old)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"func AdsResponseFromPool() *AdsResponse",
		"func (m *AdsResponse) ReturnToPool()",
		"func (m *AdsResponse) Recycle()",
		"func (m *AdsResponse) reset()",
		"func (m *AdsResponse) Clone() *AdsResponse",
		"func NestedFromPool() *Nested",
		"type EasyJSON_exporter_OnlyPool *OnlyPool",
		"func OnlyPoolFromPool() *OnlyPool",
		"func (m *OnlyPool) ReturnToPool()",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("stub missing %q\n%s", want, src)
		}
	}
	for _, absent := range []string{
		"func (m *Nested) ReturnToPool()",
		"func (m *Nested) Clone()",
		"type EasyJSON_exporter_Nested",
	} {
		if strings.Contains(src, absent) {
			t.Fatalf("stub has %q\n%s", absent, src)
		}
	}
}

func TestRunRestoresFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "p_easyjson.go")
	old := []byte("package p\nfunc Keep() {}\n")
	if err := os.WriteFile(out, old, 0644); err != nil {
		t.Fatal(err)
	}
	g := Generator{
		PkgName: "p",
		PkgPath: "example.com/missing",
		Types:   []string{"T"},
		OutName: out,
	}
	if err := g.Run(); err == nil {
		t.Fatal("expected generate to fail")
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(old) {
		t.Fatalf("file = %q", b)
	}
}
