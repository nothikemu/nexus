package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSections(t *testing.T) {
	m, err := Parse("-- header comment\n-- nexus:up\ncreate table a (id int);\n\n-- nexus:down\ndrop table a;\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(m.Up) != "create table a (id int);" || strings.TrimSpace(m.Down) != "drop table a;" {
		t.Fatalf("up=%q down=%q", m.Up, m.Down)
	}
	if m.UpLine != 3 || m.DownLine != 6 || !m.HasDown || m.NoTransaction || m.Empty {
		t.Fatalf("parsed = %+v", m)
	}
}

func TestParseWithoutMarkers(t *testing.T) {
	m, err := Parse("create table a (id int);\n")
	if err != nil {
		t.Fatal(err)
	}
	if m.HasDown || m.UpLine != 1 || !strings.Contains(m.Up, "create table") {
		t.Fatalf("parsed = %+v", m)
	}
}

func TestParseNoTransactionAndErrors(t *testing.T) {
	m, err := Parse("-- nexus:no-transaction\n-- nexus:up\ncreate index concurrently i on a (id);\n")
	if err != nil || !m.NoTransaction {
		t.Fatalf("m=%+v err=%v", m, err)
	}
	bad := []string{
		"select 1;\n-- nexus:up\nselect 2;",
		"-- nexus:up\nselect 1;\n-- nexus:up\nselect 2;",
		"-- nexus:down\ndrop table a;\n-- nexus:up\nselect 1;",
		"-- nexus:up\nbegin;\ncreate table a (id int);\ncommit;",
	}
	for _, src := range bad {
		var pe *ParseError
		if _, err := Parse(src); !errors.As(err, &pe) {
			t.Errorf("Parse(%q) err = %v, want ParseError", src, err)
		}
	}
	// Transaction control inside a function body is fine.
	if _, err := Parse("-- nexus:up\ndo $$ begin perform 1; end $$;"); err != nil {
		t.Errorf("DO block rejected: %v", err)
	}
}

func TestChecksumIgnoresWhitespaceNoise(t *testing.T) {
	a := Checksum("create table a (id int);\n")
	b := Checksum("create table a (id int);   \r\n\r\n")
	c := Checksum("create table a (id bigint);\n")
	if a != b {
		t.Error("trailing whitespace changed the checksum")
	}
	if a == c {
		t.Error("real change didn't change the checksum")
	}
}

func TestCreateAndLoadDir(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 2, 14, 15, 0, 0, time.UTC)
	p1, err := Create(dir, "Add Profiles!", now)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Create(dir, "second", now) // same second: version must still be unique
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p1) != "20261002141500_add_profiles.sql" || filepath.Base(p2) != "20261002141501_second.sql" {
		t.Fatalf("names = %s, %s", filepath.Base(p1), filepath.Base(p2))
	}
	ms, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || !ms[0].Empty || ms[0].Name != "add_profiles" {
		t.Fatalf("loaded = %+v", ms)
	}
	if err := os.WriteFile(filepath.Join(dir, "oops.sql"), []byte("select 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil {
		t.Error("expected an error for a badly named migration")
	}
}

func TestVersionLess(t *testing.T) {
	if !versionLess("9", "10") || versionLess("10", "9") || !versionLess("20260101000000", "20260101000001") {
		t.Error("versionLess ordering is wrong")
	}
}
