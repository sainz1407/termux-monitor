package database

import (
	"context"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsReadOnly covers the statement classifier that decides whether the UI
// must ask for confirmation.
func TestIsReadOnly(t *testing.T) {
	cases := []struct {
		sql  string
		want bool
	}{
		{"SELECT * FROM users", true},
		{"  select 1", true},
		{"-- comment\nSELECT 1", true},
		{"/* block */ SELECT 1", true},
		{"WITH x AS (SELECT 1) SELECT * FROM x", true},
		{"with x AS (SELECT 1) DELETE FROM t", false},
		{"EXPLAIN QUERY PLAN SELECT 1", true},
		{"VALUES (1)", true},
		{"INSERT INTO t VALUES (1)", false},
		{"update t set a = 1", false},
		{"DELETE FROM t", false},
		{"CREATE TABLE t (a)", false},
		{"DROP TABLE t", false},
		{"PRAGMA table_info(t)", false},
		{"VACUUM", false},
		{"ATTACH DATABASE 'x' AS y", false},
		{"", false},
		{"--only a comment", false},
	}
	for _, tc := range cases {
		if got := IsReadOnly(tc.sql); got != tc.want {
			t.Errorf("IsReadOnly(%q) = %v, want %v", tc.sql, got, tc.want)
		}
	}
}

// TestNormalizePath covers `~` expansion and relative resolution.
func TestNormalizePath(t *testing.T) {
	home := "/data/data/com.termux/files/home"
	cases := map[string]string{
		"~":                    home,
		"~/apps/db.sqlite":     path.Join(home, "apps/db.sqlite"),
		"apps/db.sqlite":       path.Join(home, "apps/db.sqlite"),
		"/data/local/tmp/x.db": "/data/local/tmp/x.db",
		"~/a/../b.db":          path.Join(home, "b.db"),
		"":                     "",
	}
	for in, want := range cases {
		if got := NormalizePath(home, in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResolvePathRejectsOutside makes sure a traversal cannot escape the roots.
func TestResolvePathRejectsOutside(t *testing.T) {
	svc := NewServiceWith(fakeExec{home: "/data/data/com.termux/files/home"})
	for _, p := range []string{
		"/data/data/com.android.providers.telephony/databases/mmssms.db",
		"~/../../com.android.settings/x.db",
		"/etc/passwd",
		"",
	} {
		if _, err := svc.ResolvePath(p); err == nil {
			t.Errorf("ResolvePath(%q) accepted an outside path", p)
		}
	}
	if _, err := svc.ResolvePath("~/a.db"); err != nil {
		t.Errorf("ResolvePath rejected a valid path: %v", err)
	}
}

// TestDecodeColumns parses the JSON header payload.
func TestDecodeColumns(t *testing.T) {
	got := decodeColumns(`["id","name","email"]`)
	if len(got) != 3 || got[1] != "name" {
		t.Fatalf("unexpected columns: %v", got)
	}
	// A name carrying the framing byte must survive: this is why the header is
	// JSON rather than a unit-separated list.
	if got := decodeColumns("[\"a\\u001fb\"]"); len(got) != 1 || got[0] != "a\x1fb" {
		t.Errorf("framing byte lost: %q", got)
	}
	if got := decodeColumns(""); got != nil {
		t.Errorf("expected nil for empty payload, got %v", got)
	}
	if got := decodeColumns("old\x1flist"); len(got) != 2 {
		t.Errorf("expected fallback split, got %v", got)
	}
}

// TestDecodeRowsDetectsTruncation checks the LIMIT+1 convention.
func TestDecodeRowsDetectsTruncation(t *testing.T) {
	raw := [][]string{{`[1,"a"]`}, {`[2,"b"]`}, {`[3,"c"]`}}
	rows, truncated := decodeRows(raw, 2, 2)
	if !truncated || len(rows) != 2 {
		t.Fatalf("truncated=%v rows=%d", truncated, len(rows))
	}
	if rows[0][1] != "a" {
		t.Errorf("unexpected first row: %v", rows[0])
	}
}

// TestDecodeRowsKeepsValueClasses proves a JSON row keeps NULL apart from empty
// text and survives framing bytes verbatim, and that a malformed payload never
// leaks into the table.
func TestDecodeRowsKeepsValueClasses(t *testing.T) {
	raw := [][]string{{"[null,\"\",\"a\\nb\\u001fc\"]"}, {`[null]`}, {`not json`}}
	rows, truncated := decodeRows(raw, 3, 10)
	if truncated || len(rows) != 3 {
		t.Fatalf("truncated=%v rows=%d", truncated, len(rows))
	}
	first := rows[0]
	if first[0] != nil || first[1] != "" || first[2] != "a\nb\x1fc" {
		t.Errorf("unexpected first row: %#v", first)
	}
	if len(first) != 3 {
		t.Errorf("row wider than the header: %#v", first)
	}
	if len(rows[1]) != 3 || rows[1][1] != nil || rows[1][2] != nil {
		t.Errorf("short row not padded to the header width: %#v", rows[1])
	}
	if len(rows[2]) != 3 || rows[2][0] != nil {
		t.Errorf("malformed payload not neutralised: %#v", rows[2])
	}
}

// TestDecodeCellRoundTrip proves the quote()+replaceChain framing survives every
// value class that could otherwise break one row onto several lines.
func TestDecodeCellRoundTrip(t *testing.T) {
	cases := []struct {
		raw  string
		want any
	}{
		{"", nil},
		{"NULL", nil},
		{"'alpha'", "alpha"},
		{"9.5", "9.5"},
		{"'it''s'", "it's"},
		{"'a\\nb'", "a\nb"},
		{"'a\\rb'", "a\rb"},
		{"'a\\ub'", "a\x1fb"},
		{"'c:\\\\dir'", `c:\dir`},
		{"X'00ff'", "00ff"},
	}
	for _, tc := range cases {
		if got := decodeCell(tc.raw); got != tc.want {
			t.Errorf("decodeCell(%q) = %#v, want %#v", tc.raw, got, tc.want)
		}
	}
}

// TestExtractRowsKeepsNullRow makes sure a row whose every cell is NULL is not
// mistaken for noise printed by the CLI.
func TestExtractRowsKeepsNullRow(t *testing.T) {
	raw := "warning: something\n" + rowsMarker + `[1,"alpha"]` + "\n" +
		rowsMarker + "[null,null]\n"
	rows := extractRows(raw, rowsMarker)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if len(rows[0]) != 1 || rows[0][0] != `[1,"alpha"]` {
		t.Errorf("unexpected first row: %v", rows[0])
	}
	decoded, _ := decodeRows(rows, 2, 10)
	if len(decoded[0]) != 2 || decoded[0][0] != float64(1) || decoded[0][1] != "alpha" {
		t.Errorf("unexpected decoded row: %v", decoded[0])
	}
	if len(decoded[1]) != 2 || decoded[1][0] != nil || decoded[1][1] != nil {
		t.Errorf("NULL row not preserved: %v", decoded[1])
	}
}

// TestTrimCellKeepsBrowserResponsive checks the per-cell cap.
func TestTrimCellKeepsBrowserResponsive(t *testing.T) {
	long := strings.Repeat("x", MaxTextLen+50)
	out := trimCell(long)
	if len(out) <= MaxTextLen {
		t.Errorf("expected truncation marker, got length %d", len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("missing marker in %q", out[len(out)-40:])
	}
}

// TestRowLimitClamps covers the request bounds.
func TestRowLimitClamps(t *testing.T) {
	if RowLimit(0) != DefaultRowLimit {
		t.Errorf("expected default for 0")
	}
	if RowLimit(-5) != DefaultRowLimit {
		t.Errorf("expected default for negative")
	}
	if RowLimit(MaxRowLimit*10) != MaxRowLimit {
		t.Errorf("expected clamp to max")
	}
	if RowLimit(50) != 50 {
		t.Errorf("expected passthrough")
	}
}

// TestProbeRowsScript checks the generated SQL: the probe view, the header
// marker, the row marker with the LIMIT+1 convention and the cleanup ordering.
func TestProbeRowsScript(t *testing.T) {
	probe := columnProbeSQL("SELECT 1")
	if !strings.Contains(probe, "CREATE TEMP VIEW "+probeView) {
		t.Errorf("probe view not created: %s", probe)
	}
	if !strings.Contains(probe, quoteLiteral(columnsMarker)) || !strings.Contains(probe, "json_group_array(name)") {
		t.Errorf("missing header marker: %s", probe)
	}
	rows := rowsSQL(10)
	if !strings.Contains(rows, quoteLiteral(rowsMarker)) || !strings.Contains(rows, "LIMIT 11") {
		t.Errorf("expected rows marker and LIMIT limit+1: %s", rows)
	}
	explicit := rowsSQLFor([]string{"id", `odd "name"`, "value"}, 5)
	if !strings.Contains(explicit, `json_array(c."id", c."odd ""name""", c."value")`) {
		t.Errorf("columns must be quoted into the projection: %s", explicit)
	}
	if !strings.Contains(explicit, "LIMIT 6") {
		t.Errorf("expected LIMIT limit+1: %s", explicit)
	}
	script := probe + guardSQL() + rows + "\n.bail off\nPRAGMA query_only=OFF;\n" + viewDropperSQL()
	if !strings.Contains(script, "DROP VIEW IF EXISTS temp."+probeView) {
		t.Errorf("expected view cleanup: %s", script)
	}
	if strings.Index(script, "PRAGMA query_only=ON") > strings.Index(script, "DROP VIEW") {
		t.Errorf("view dropped before query_only was disabled: %s", script)
	}
	if strings.Index(script, quoteLiteral(columnsMarker)) > strings.Index(script, rowsMarker) {
		t.Errorf("header must be printed before the rows: %s", script)
	}
}

// TestQueryScriptRunsUnderQueryOnly executes the generated helper script with
// the system sqlite3 CLI when one is available, to prove the envelope itself
// (markers, JSON header, dynamic aggregate, view cleanup) is valid SQL.
func TestQueryScriptRunsUnderQueryOnly(t *testing.T) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 CLI not available")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "probe.db")
	setup := "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, note TEXT, score REAL);\n" +
		"INSERT INTO t (name, note, score) VALUES ('alpha', 'it''s' || char(10) || 'two', 9.5), ('gamma', NULL, NULL);\n"
	if out, err := exec.Command(bin, dbPath, setup).CombinedOutput(); err != nil {
		t.Fatalf("seed failed: %v: %s", err, out)
	}
	stmt := "SELECT id AS \"my id\", name, note, score FROM t ORDER BY id"
	run := func(script string) string {
		cmd := exec.Command(bin, "-batch", "-bail", "-noheader", dbPath)
		cmd.Stdin = strings.NewReader(script)
		var errBuf strings.Builder
		cmd.Stderr = &errBuf
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("helper script failed: %v: %s", err, errBuf.String())
		}
		return string(out)
	}
	// Phase one only learns the header; phase two prints the rows with the
	// explicit projection, exactly like the service does.
	header := run(columnProbeSQL(stmt) + guardSQL() + rowsSQLFor(nil, 0) +
		"\n.bail off\nPRAGMA query_only=OFF;\n" + viewDropperSQL())
	columns := decodeColumns(mustMarker(header, columnsMarker))
	if len(columns) != 4 || columns[0] != "my id" {
		t.Fatalf("unexpected columns %v from %q", columns, header)
	}
	text := run(columnProbeSQL(stmt) + guardSQL() + rowsSQLFor(columns, 10) +
		"\n.bail off\nPRAGMA query_only=OFF;\n" + viewDropperSQL())
	rows, truncated := decodeRows(extractRows(text, rowsMarker), len(columns), 10)
	if truncated || len(rows) != 2 {
		t.Fatalf("unexpected rows: %v truncated=%v", rows, truncated)
	}
	if rows[0][0] != float64(1) || rows[0][1] != "alpha" || rows[0][2] != "it's\ntwo" {
		t.Errorf("unexpected first row: %#v", rows[0])
	}
	if rows[0][3] != 9.5 {
		t.Errorf("unexpected REAL cell: %#v", rows[0][3])
	}
	if rows[1][2] != nil || rows[1][3] != nil {
		t.Errorf("NULL not preserved: %#v", rows[1])
	}
	if strings.Contains(text, probeView+"\x00") {
		t.Errorf("view left behind: %q", text)
	}
}

// TestParseSchema folds marker rows.
func TestParseSchema(t *testing.T) {
	raw := strings.Join([]string{
		"ddl\x1fCREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)",
		"col\x1f0\x1fid\x1fINTEGER\x1f0\x1f\x1f1",
		"col\x1f1\x1fname\x1fTEXT\x1f1\x1f\x1f0",
		"idx\x1fidx_name\x1f1",
		"col2\x1fidx_name\x1fname",
	}, "\n")
	sc := parseSchema("t", raw)
	if len(sc.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(sc.Columns))
	}
	if !sc.Columns[0].PK || !sc.Columns[1].NotNull {
		t.Errorf("flags not decoded: %+v", sc.Columns)
	}
	if len(sc.Indexes) != 1 || !sc.Indexes[0].Unique || sc.Indexes[0].Columns[0] != "name" {
		t.Errorf("index not decoded: %+v", sc.Indexes)
	}
	if !strings.Contains(sc.SQL, "CREATE TABLE") {
		t.Errorf("ddl not captured: %q", sc.SQL)
	}
}

// fakeExec is a no-op executor used by the path tests.
type fakeExec struct{ home string }

func (f fakeExec) Exec(_ context.Context, _, _ string, _ bool) (string, string, error) {
	return "", "", nil
}

func (f fakeExec) HasSQLite() bool { return true }

func (f fakeExec) Home() string { return f.home }
