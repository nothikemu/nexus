package sqltext

import (
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	script := `
-- leading comment
create table a (id int); -- trailing comment
insert into a values (1), (2);

/* block /* nested */ comment; still comment */
select 'it''s; fine', E'back\'slash; ok', "weird;ident" from a;

create function f() returns int language plpgsql as $body$
begin
  return 1; -- semicolon inside a dollar-quoted body
end
$body$;

create function g(x int) returns int language sql
begin atomic
  select case when x > 0 then 1 else 0 end;
  select 2;
end;

select $1::int;
`
	stmts := Split(script)
	var got []string
	for _, s := range stmts {
		got = append(got, strings.Fields(s.SQL)[0]+" "+strings.Fields(s.SQL)[1])
	}
	want := []string{"create table", "insert into", "select 'it''s;", "create function", "create function", "select $1::int"}
	if len(got) != len(want) {
		t.Fatalf("got %d statements %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d = %q, want %q", i, got[i], want[i])
		}
	}
	if stmts[0].Line != 3 {
		t.Errorf("first statement line = %d, want 3", stmts[0].Line)
	}
	if !strings.HasSuffix(stmts[4].SQL, "end") {
		t.Errorf("BEGIN ATOMIC body split incorrectly: %q", stmts[4].SQL)
	}
}

func TestSplitTrailingStatementWithoutSemicolon(t *testing.T) {
	stmts := Split("select 1; select 2")
	if len(stmts) != 2 || stmts[1].SQL != "select 2" {
		t.Fatalf("got %+v", stmts)
	}
}

func TestComplete(t *testing.T) {
	cases := map[string]bool{
		"select 1;":                        true,
		"select 1":                         false,
		"select 1; -- done":                true,
		"select ';'":                       false,
		"select 'unterminated;":            false,
		"create function f() as $$ x;":     false,
		"create function f() as $$ x; $$;": true,
		"   ":                              false,
		"-- just a comment;":               false,
		"/* open comment ;":                false,
		"select 1;\nselect 2;":             true,
	}
	for in, want := range cases {
		if got := Complete(in); got != want {
			t.Errorf("Complete(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLineColAndCharToByte(t *testing.T) {
	src := "select\n  é, x"
	b := CharToByte(src, 11) // 1-based char 11 is 'x'... s,e,l,e,c,t,\n, , ,é,,
	if src[b:b+1] != "," {
		t.Fatalf("CharToByte = %d (%q)", b, src[b:])
	}
	line, col := LineCol(src, b)
	if line != 2 || col != 4 {
		t.Errorf("LineCol = %d:%d, want 2:4", line, col)
	}
}
