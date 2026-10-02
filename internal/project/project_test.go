package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScaffoldAndFind(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shop")
	res, err := Scaffold(ScaffoldOptions{Dir: root, Name: "shop", Now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"nexus.yaml", filepath.Join("migrations", "20261002120000_init.sql"), filepath.Join("seeds", "seed.sql"), ".gitignore"}
	if strings.Join(res.Created, ",") != strings.Join(want, ",") {
		t.Errorf("created = %v", res.Created)
	}
	// Found from a nested directory.
	nested := filepath.Join(root, "seeds")
	p, err := Find(nested)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "shop" || p.Root != root || len(p.Warnings) != 0 {
		t.Errorf("project = %+v", p)
	}
	files, err := p.SeedFiles()
	if err != nil || len(files) != 1 {
		t.Errorf("seed files = %v, %v", files, err)
	}
	if _, err := Scaffold(ScaffoldOptions{Dir: root, Name: "shop"}); !errors.Is(err, ErrExists) {
		t.Errorf("second scaffold = %v", err)
	}
}

func TestScaffoldKeepsExistingGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Scaffold(ScaffoldOptions{Dir: root, Name: "app", Blank: true})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if !strings.HasPrefix(string(data), "node_modules\n") || !IgnoresStateDir(string(data)) {
		t.Errorf(".gitignore = %q", data)
	}
	if len(res.Updated) != 1 || len(res.Created) != 1 {
		t.Errorf("created=%v updated=%v (blank projects have no starter files)", res.Created, res.Updated)
	}
}

func TestFindOutsideProject(t *testing.T) {
	if _, err := Find(t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestLocalStateIsPrivateAndStable(t *testing.T) {
	root := t.TempDir()
	if _, err := Scaffold(ScaffoldOptions{Dir: root, Name: "app", Blank: true}); err != nil {
		t.Fatal(err)
	}
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	a, err := p.LocalState()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := p.LocalState()
	if a.Password == "" || a.Password != b.Password || len(a.Password) != 32 {
		t.Errorf("password not stable: %q vs %q", a.Password, b.Password)
	}
	fi, err := os.Stat(filepath.Join(root, StateDirName, "local.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("local.json mode = %v, %v", fi.Mode().Perm(), err)
	}
}
