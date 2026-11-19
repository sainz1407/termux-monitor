// Package database implements the Database tab backend for SQLite files that
// live on the phone. The server itself is a static Go binary running as root
// from a Magisk module, so it cannot link a C SQLite driver without cgo.
// Instead every statement is executed by the `sqlite3` CLI that ships with
// Termux, as the Termux application user. That keeps the binary portable and
// guarantees a database is only ever touched by the identity that owns it.
package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"termux-monitor/internal/store"
)

// Errors surfaced to the UI. The handler maps them to HTTP status codes.
var (
	ErrOutside       = errors.New("path is outside the allowed roots")
	ErrSQLiteMissing = errors.New("sqlite3 binary not found in Termux $PREFIX")
	ErrBinaryFile    = errors.New("file is not an SQLite database")
	ErrEmptySQL      = errors.New("SQL statement is required")
	ErrTimeout       = errors.New("sqlite3 did not finish in time")
)

// Executor runs one SQLite CLI invocation. It exists so tests can replace the
// Termux subprocess with a fake.
type Executor interface {
	// Exec runs script with sqlite3 against dbPath and returns stdout and
	// stderr separately.
	Exec(ctx context.Context, dbPath, script string, readOnly bool) (stdout, stderr string, err error)
	// HasSQLite reports whether the sqlite3 binary is available.
	HasSQLite() bool
	// Home returns the Termux home directory, used to expand `~`.
	Home() string
}

// TermuxExecutor runs sqlite3 as the Termux user.
type TermuxExecutor struct{}

// NewExecutor returns the production executor.
func NewExecutor() *TermuxExecutor { return &TermuxExecutor{} }

// Home returns the Termux HOME directory.
func (TermuxExecutor) Home() string { return store.TermuxHome }

// HasSQLite reports whether sqlite3 is resolvable in $PREFIX/bin. A missing
// binary is not an error state: the UI shows the `pkg install sqlite` hint.
func (TermuxExecutor) HasSQLite() bool {
	ctx, cancel := context.WithTimeout(context.Background(), store.ProbeTimeout)
	defer cancel()
	cmd, err := store.TermuxCommand(ctx, executable(SQLiteBinary)+" -version")
	if err != nil {
		return false
	}
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// Exec runs script through the SQLite CLI. The script is piped on stdin so the
// SQL never has to survive shell quoting.
//
// Read-only mode does not use the `-readonly` flag: the Android/Termux build of
// the CLI is compiled without it, so passing it aborts every invocation. The
// same guarantee is expressed in SQL instead: a request that needs a helper
// script opens the envelope itself (see probeRowsSQL), and a bare script is
// wrapped here in a savepoint over a query-only connection that is always
// rolled back.
func (TermuxExecutor) Exec(ctx context.Context, dbPath, script string, readOnly bool) (string, string, error) {
	if readOnly && !strings.Contains(script, savepointGuard) {
		script = readOnlyPrologue + guardSQL() + script + queryOnlyEpilogue
	}
	cmd, err := store.TermuxCommand(ctx, executable(SQLiteBinary)+" -batch -bail -noheader "+shellQuote(dbPath))
	if err != nil {
		return "", "", err
	}
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.String(), stderr.String(), err
}

// SQLiteBinary is the CLI the executor looks for inside Termux $PREFIX/bin.
// The absolute path is used instead of the bare name because an Android su
// shell leaks /system/bin into PATH, where a different `sqlite3` (the platform
// one, without JSON or temp-store support) would otherwise win the lookup.
const SQLiteBinary = "sqlite3"

// executable returns the absolute Termux path of a $PREFIX/bin binary.
func executable(name string) string {
	return store.TermuxPrefix + "/bin/" + name
}

// readOnlyPrologue opens the read-only envelope for a bare script: a savepoint
// that makes the whole run transactional and a query-only switch, so the user
// statement cannot mutate anything. It is closed by queryOnlyEpilogue.
const readOnlyPrologue = ".bail off\nSAVEPOINT tmon_readonly;\n"

// savepointGuard marks a script that already opens and closes the envelope
// itself. Exec must not wrap it a second time.
const savepointGuard = "SAVEPOINT tmon_readonly;"

// queryOnlyEpilogue rolls the savepoint back and restores the write flag.
const queryOnlyEpilogue = "\n.bail on\nROLLBACK TO tmon_readonly;\nRELEASE tmon_readonly;\nPRAGMA query_only=OFF;\n"

// guardSQL switches a script that already created its temporary view over to
// query-only, immediately after the view definition.
func guardSQL() string { return ".bail on\nPRAGMA query_only=ON;\n" }

// shellQuote wraps s in single quotes for `sh`, escaping embedded quotes.
// strconv.Quote is not used because Termux's shell does not enable $'...'.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Validation bounds.
const (
	ReadTimeout  = 30 * time.Second
	WriteTimeout = 60 * time.Second
	// MaxSQLLength caps the payload accepted from the browser.
	MaxSQLLength = 64 * 1024
	// DefaultRowLimit is used when the caller does not ask for one.
	DefaultRowLimit = 200
	// MaxRowLimit bounds how many rows a single query may return.
	MaxRowLimit = 1000
	// MaxTextLen truncates a single cell in the JSON answer.
	MaxTextLen = 4096
	// MaxTables caps the table listing of pathological databases.
	MaxTables = 500
)

// Table is one user table or view with its row count.
type Table struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// Rows is -1 when counting was skipped or failed.
	Rows int64  `json:"rows"`
	SQL  string `json:"sql,omitempty"`
}

// Column is one column of a table, as reported by pragma_table_info.
type Column struct {
	CID     int    `json:"cid"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	NotNull bool   `json:"notNull"`
	Default string `json:"default,omitempty"`
	PK      bool   `json:"pk"`
}

// Index is one index of a table.
type Index struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
}

// Schema is the full description of one table.
type Schema struct {
	Table   string   `json:"table"`
	SQL     string   `json:"sql"`
	Columns []Column `json:"columns"`
	Indexes []Index  `json:"indexes"`
}

// QueryResult is the decoded answer of one statement.
type QueryResult struct {
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	RowCount   int      `json:"rowCount"`
	Truncated  bool     `json:"truncated"`
	Changes    int64    `json:"changes"`
	DurationMs int64    `json:"durationMs"`
}

// quoteLiteral wraps s in single quotes for SQLite, doubling inner quotes.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// The temporary view that wraps a user statement during introspection. One CLI
// session creates the view, learns its columns and serialises its rows, then
// drops the view again before the savepoint is rolled back. A single session is
// required: TEMP objects live in the connection and die with it.
//
// Results are separated by in-band marker lines instead of the CLI `.once`
// command: `.once` writes to a file, and the Android build of the Termux CLI
// has no `-readonly`, so the read-only guarantee is expressed in SQL and no
// file may be created.
const (
	probeView     = "__tmon_probe"
	columnsMarker = "__TMON_COLUMNS__"
	rowsMarker    = "__TMON_ROWS__"
)

// columnProbeSQL creates the temporary view over the user statement and prints
// only its column list. Callers must not run this script through Exec: dropping
// the temporary view is a write that query-only mode rejects. The matching
// viewDropperSQL releases the view on the connection that still holds it.
func columnProbeSQL(userSQL string) string {
	return ".mode list\n" +
		"CREATE TEMP VIEW " + probeView + " AS " + userSQL + ";\n" +
		"SELECT " + quoteLiteral(columnsMarker) + " || " +
		columnListSQL(probeView) + ";\n"
}

// columnListSQL builds the JSON array of the column names of a view. JSON is
// used instead of a plain unit-separated list because a column name may legally
// contain any byte, including the separator itself, and only a quoted JSON
// string can carry it unambiguously to Go.
func columnListSQL(view string) string {
	return "json_group_array(name) FROM pragma_table_info(" + quoteLiteral(view) + ") ORDER BY cid"
}

// viewDropperSQL releases the temporary view left by columnProbeSQL. Dropping a
// TEMP object is a write, so the script must run without the read-only envelope.
func viewDropperSQL() string {
	return "DROP VIEW IF EXISTS temp." + probeView + ";\n"
}

// rowSeparator joins the cells of one printed row. It is the unit separator so
// a printable value can never collide with the framing.
const rowSeparator = "\x1f"

// rowsSQL prints one line per view row: the rows marker, then one JSON array of
// that row's cells. The view itself prints the array, so the projection never has
// to know a column name that needs quoting.
//
// A JSON array is used because it already separates the cells unambiguously:
// strings, numbers and NULLs are all distinct in it, so no per-cell escaping is
// needed and a value may contain any byte (newlines, the unit separator, quotes)
// without breaking the row framing. sqlite3 prints one array per line, so one
// line stays one row.
//
// A view projection does not carry the caller's ORDER BY, so the rows keep the
// order the engine returned them in. LIMIT is one higher than requested so the
// caller can detect truncation.
func rowsSQL(limit int) string {
	return rowsSQLFor(nil, limit)
}

// rowsSQLFor is rowsSQL with an explicit column list, because SQLite cannot
// expand `json_array(*)` into one argument per column and cannot take a
// dynamically computed argument list either. The columns come from the header
// probe that already ran on this connection, so the projection names exactly
// the columns of the view. An empty list falls back to the bare row form.
func rowsSQLFor(columns []string, limit int) string {
	b := &strings.Builder{}
	b.WriteString(".mode list\n")
	args := "*"
	if len(columns) > 0 {
		parts := make([]string, 0, len(columns))
		// c. is the row alias, so a column may be named like a function or a
		// keyword without becoming ambiguous.
		for _, name := range columns {
			parts = append(parts, "c."+quoteIdentifier(name))
		}
		args = strings.Join(parts, ", ")
	}
	b.WriteString("SELECT " + quoteLiteral(rowsMarker) + " || json_array(" + args + ") FROM " +
		probeView + " AS c LIMIT " + strconv.Itoa(limit+1) + ";\n")
	return b.String()
}

// quoteIdentifier wraps s in double quotes for SQLite, doubling inner quotes.
// SQLite accepts a double-quoted string as an identifier position, so a column
// name that itself contains a quote, a space or a newline is still safe.
func quoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// replaceChain makes one token framing-safe. The backslash escape must run
// before the other substitutions, so every later replacement is already
// protected, and the decoder undoes the steps in reverse order.
func replaceChain(inner string) string {
	return "replace(replace(replace(replace(" + inner + ", '\\', '\\\\'), " +
		quoteLiteral(rowSeparator) + ", '\\u'), char(10), '\\n'), char(13), '\\r')"
}

// decodeCell undoes quote() plus replaceChain. SQL NULL is reported as nil so
// the browser can render it apart from the text "null"; a quoted literal is a
// number, a blob or a string.
func decodeCell(raw string) any {
	if raw == "" || raw == "NULL" {
		return nil
	}
	return trimCell(unquoteSQL(raw))
}

// unquoteSQL reverses SQLite's quote(): 'text', X'hex' and bare numbers.
func unquoteSQL(raw string) string {
	if strings.HasPrefix(raw, "X'") && strings.HasSuffix(raw, "'") && len(raw) >= 3 {
		return raw[2 : len(raw)-1]
	}
	if len(raw) < 2 || raw[0] != '\'' || raw[len(raw)-1] != '\'' {
		return raw
	}
	body := raw[1 : len(raw)-1]
	body = strings.ReplaceAll(body, "''", "'")
	body = strings.ReplaceAll(body, `\u`, rowSeparator)
	body = strings.ReplaceAll(body, `\n`, "\n")
	body = strings.ReplaceAll(body, `\r`, "\r")
	body = strings.ReplaceAll(body, `\\`, `\`)
	return body
}

// extractRows collects the marker-prefixed row lines of a row script. The
// payload is the whole line after the marker (one JSON array per row), so a row
// of NULLs still yields a payload and is never dropped.
func extractRows(raw, marker string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		payload, ok := strings.CutPrefix(line, marker)
		if !ok {
			continue
		}
		rows = append(rows, []string{payload})
	}
	return rows
}

// extractMarker returns the first line written after marker, so a diagnostic
// value such as a column header can never absorb the lines that follow it.
func extractMarker(raw, marker string) (string, bool) {
	idx := strings.Index(raw, marker)
	if idx < 0 {
		return "", false
	}
	rest := raw[idx+len(marker):]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest), true
}

// decodeColumns splits the columns marker payload, a JSON array produced by
// columnListSQL. A payload that is not valid JSON is treated as a plain
// unit-separated list so a diagnostic run still yields something readable.
func decodeColumns(payload string) []string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil
	}
	if strings.HasPrefix(payload, "[") {
		var names []any
		if err := json.Unmarshal([]byte(payload), &names); err == nil {
			out := make([]string, 0, len(names))
			for _, v := range names {
				if s, ok := v.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			return out
		}
	}
	parts := strings.Split(payload, rowSeparator)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// RowLimit normalises a requested row limit.
func RowLimit(n int) int {
	if n <= 0 {
		return DefaultRowLimit
	}
	if n > MaxRowLimit {
		return MaxRowLimit
	}
	return n
}

// trimCell keeps a value displayable: long TEXT is cut and BLOBs are replaced
// by a size marker, because the browser table must stay responsive.
func trimCell(v string) string {
	if len(v) > MaxTextLen {
		return v[:MaxTextLen] + fmt.Sprintf("… (%d bytes truncated)", len(v)-MaxTextLen)
	}
	return v
}

// decodeRows turns the printed row lines into display values. Every source row
// is one JSON array printed after the marker; the array carries exactly the
// cells the projection selected, and `columnCount` pads a short row so the table
// always matches its header.
func decodeRows(raw [][]string, columnCount, limit int) ([][]any, bool) {
	truncated := len(raw) > limit
	if truncated {
		raw = raw[:limit]
	}
	rows := make([][]any, 0, len(raw))
	for _, src := range raw {
		var cells []any
		if len(src) > 0 {
			if err := json.Unmarshal([]byte(src[0]), &cells); err != nil {
				cells = nil
			}
		}
		row := make([]any, 0, columnCount)
		for _, cell := range cells {
			row = append(row, cell)
		}
		for len(row) < columnCount {
			row = append(row, nil)
		}
		if len(row) > columnCount {
			row = row[:columnCount]
		}
		rows = append(rows, row)
	}
	return rows, truncated
}

// NormalizePath resolves `~` and makes the path absolute without touching the
// filesystem, so validation can run before any stat.
func NormalizePath(home, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" {
		return path.Clean(home)
	}
	if strings.HasPrefix(p, "~/") {
		return path.Clean(path.Join(home, p[2:]))
	}
	if !path.IsAbs(p) {
		return path.Clean(path.Join(home, p))
	}
	return path.Clean(p)
}
