package pg

import "testing"

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"postgres://postgres:s3cret@127.0.0.1:54320/app?sslmode=disable": "postgres://postgres:•••••@127.0.0.1:54320/app?sslmode=disable",
		"postgres://u:p%40ss@host/db":                                    "postgres://u:•••••@host/db",
		"postgres://u@host/db":                                           "postgres://u@host/db",
		"host=localhost user=x":                                          "host=localhost user=x",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDescribe(t *testing.T) {
	d := Describe("postgres://postgres:pw@127.0.0.1:54320/app")
	if d.String() != "postgres@127.0.0.1:54320/app" {
		t.Errorf("Describe = %q", d.String())
	}
}
