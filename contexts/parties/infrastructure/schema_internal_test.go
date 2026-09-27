package infrastructure

import "testing"

func TestRender(t *testing.T) {
	ddl := `ALTER TABLE parties {add:shared} {bool} DEFAULT {false} NOT NULL{addEnd}`
	want := map[string]string{
		"sqlite":    "ALTER TABLE parties ADD COLUMN shared INTEGER DEFAULT 0 NOT NULL",
		"postgres":  "ALTER TABLE parties ADD COLUMN shared BOOLEAN DEFAULT FALSE NOT NULL",
		"sqlserver": "ALTER TABLE parties ADD shared BIT DEFAULT 0 NOT NULL",
		"oracle":    "ALTER TABLE parties ADD (shared NUMBER(1) DEFAULT 0 NOT NULL)",
		"mysql":     "ALTER TABLE parties ADD COLUMN shared BOOLEAN DEFAULT FALSE NOT NULL",
	}
	for d, w := range want {
		if got := render(d, ddl); got != w {
			t.Errorf("%s:\n got %s\nwant %s", d, got, w)
		}
	}
	if got := render("oracle", "x {str:30} {uuid}"); got != "x VARCHAR2(30) RAW(16)" {
		t.Fatal(got)
	}
}
