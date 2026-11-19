package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"termux-monitor/internal/config"
	"termux-monitor/internal/store"
)

// Database file extensions offered in the file picker. SQLite accepts any
// name, so the list is a convenience filter rather than a hard rule.
var dbExtensions = []string{".db", ".sqlite", ".sqlite3", ".db3", ".s3db"}

// Status is the header payload of the tab.
type Status struct {
	// Available is false when the Termux environment cannot run commands.
	Available bool `json:"available"`
	// Reason explains an unavailable environment.
	Reason string `json:"reason"`
	// SQLiteInstalled reports whether `sqlite3` is in $PREFIX.
	SQLiteInstalled bool `json:"sqliteInstalled"`
	// Home is the Termux HOME shown in the breadcrumb.
	Home string `json:"home"`
	// Roots lists the directories the picker may browse.
	Roots []string `json:"roots"`
	// Version is the `sqlite3 --version` first line, empty when unknown.
	Version string `json:"version"`
}

// DirEntry is one row of the file picker.
type DirEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	// Database is true when the file starts with the SQLite magic header.
	Database bool `json:"database"`
}

// Listing is the answer of GET /api/database/files.
type Listing struct {
	Path    string     `json:"path"`
	Parent  string     `json:"parent"`
	Crumbs  []Crumb    `json:"crumbs"`
	Entries []DirEntry `json:"entries"`
}

// Crumb is one clickable segment of the breadcrumb.
type Crumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Service is the Database tab domain object.
type Service struct {
	exec Executor
}

// NewService returns a Service backed by the Termux SQLite CLI.
func NewService() *Service { return &Service{exec: NewExecutor()} }

// NewServiceWith returns a Service using an injected executor for tests.
func NewServiceWith(exec Executor) *Service { return &Service{exec: exec} }

// sqliteHeader is the magic prefix at offset 0 of every SQLite 3 database.
const sqliteHeader = "SQLite format 3\x00"

// Roots returns the directories the picker may browse: the Termux home, the
// module data directory and the shared temp directory. Everything else is
// rejected, so a crafted path cannot reach system databases.
func (s *Service) Roots() []string {
	roots := []string{s.exec.Home(), "/data/local/tmp"}
	if dir := config.GetPersistentDataDir(); dir != "" {
		roots = append(roots, dir)
	}
	return roots
}

// Status reports environment capability for the tab header.
func (s *Service) Status() Status {
	st := Status{Home: s.exec.Home(), Roots: s.Roots()}
	if !store.Available() {
		st.Reason = store.AvailableReason()
		return st
	}
	st.Available = true
	st.SQLiteInstalled = s.exec.HasSQLite()
	if st.SQLiteInstalled {
		st.Version = s.sqliteVersion()
	}
	return st
}

// sqliteVersion reads the CLI version, best effort only.
func (s *Service) sqliteVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), store.ProbeTimeout)
	defer cancel()
	cmd, err := store.TermuxCommand(ctx, "sqlite3 --version")
	if err != nil {
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}

// ResolvePath validates a requested database path and returns its clean form.
// The path must resolve inside one of the allowed roots.
func (s *Service) ResolvePath(p string) (string, error) {
	clean := NormalizePath(s.exec.Home(), p)
	if clean == "" {
		return "", ErrOutside
	}
	if !s.insideRoots(clean) {
		return "", ErrOutside
	}
	return clean, nil
}

// insideRoots reports whether p is inside one of the allowed roots.
func (s *Service) insideRoots(p string) bool {
	for _, root := range s.Roots() {
		if root == "" {
			continue
		}
		root = path.Clean(root)
		if p == root {
			return true
		}
		if strings.HasPrefix(p, root+"/") {
			return true
		}
	}
	return false
}

// Reroot rebases p under root when the stored path is absolute but no longer
// inside any allowed root. The Database tab resolves paths on the host
// filesystem first, but the Termux HOME differs between an emulator, a test
// fixture and a real device; remounting the same relative subpath keeps a
// saved picker selection usable instead of failing as "outside the roots".
func (s *Service) Reroot(p, root string) string {
	if strings.TrimSpace(p) == "" || root == "" {
		return p
	}
	clean := path.Clean(p)
	if path.IsAbs(clean) && s.insideRoots(clean) {
		return p
	}
	return s.rerootTo(p, root)
}

// relativeToAnyRoot returns p relative to the first allowed root that contains
// it.
func relativeToAnyRoot(p string, roots []string) (string, bool) {
	p = path.Clean(p)
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = path.Clean(root)
		if p == root {
			return ".", true
		}
		if strings.HasPrefix(p, root+"/") {
			return strings.TrimPrefix(p, root+"/"), true
		}
	}
	return "", false
}

// rerootIfNeeded rebases p under the primary root when it is absolute but
// outside every allowed root.
func (s *Service) rerootIfNeeded(p string) string {
	if strings.TrimSpace(p) == "" {
		return p
	}
	clean := path.Clean(p)
	if !path.IsAbs(clean) || s.insideRoots(clean) {
		return p
	}
	root := s.Roots()[0]
	if rebased := s.rerootTo(p, root); rebased != clean {
		return rebased
	}
	return p
}

// rerootTo returns p rebased under one specific root, or clean p when no
// relative subpath can be derived.
func (s *Service) rerootTo(p, root string) string {
	clean := path.Clean(p)
	sub := ""
	if rel, ok := relativeToAnyRoot(clean, s.Roots()); ok {
		sub = rel
	} else if !path.IsAbs(clean) {
		sub = clean
	}
	if sub == "" {
		return clean
	}
	candidate := path.Clean(path.Join(root, sub))
	if !s.insideRoots(candidate) {
		return clean
	}
	return candidate
}

// ListDir returns the directories and database files of one directory.
func (s *Service) ListDir(p string) (*Listing, error) {
	p = s.rerootIfNeeded(p)
	dir, err := s.ResolvePath(p)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && path.IsAbs(strings.TrimSpace(p)) {
			// Recorded roots can differ between host and device (for example a
			// test fixture HOME); fall back to the primary root.
			if rebased := s.rerootTo(p, s.Roots()[0]); rebased != dir {
				if retry, retryErr := os.ReadDir(rebased); retryErr == nil {
					dir, entries, err = rebased, retry, nil
				}
			}
		}
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: directory does not exist", ErrOutside)
		}
		return nil, err
	}
	out := &Listing{Path: dir, Parent: s.parentOf(dir), Crumbs: s.crumbs(dir)}
	for _, e := range entries {
		name := e.Name()
		full := path.Join(dir, name)
		if e.IsDir() {
			out.Entries = append(out.Entries, DirEntry{Name: name, Path: full, IsDir: true})
			continue
		}
		if !isSuspectedDB(name) {
			continue
		}
		info, statErr := e.Info()
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		out.Entries = append(out.Entries, DirEntry{
			Name:     name,
			Path:     full,
			Size:     info.Size(),
			ModTime:  info.ModTime().UnixMilli(),
			Database: hasSQLiteHeader(full),
		})
	}
	// Real databases first, then directories, then look-alikes.
	sortEntries(out.Entries)
	return out, nil
}

// parentOf returns the parent directory, clamped to the nearest root.
func (s *Service) parentOf(dir string) string {
	for _, root := range s.Roots() {
		root = path.Clean(root)
		if dir == root {
			return ""
		}
	}
	parent := path.Dir(dir)
	if !s.insideRoots(parent) {
		return dir
	}
	return parent
}

// crumbs builds the breadcrumb trail from the nearest root to dir.
func (s *Service) crumbs(dir string) []Crumb {
	var out []Crumb
	for _, root := range s.Roots() {
		root = path.Clean(root)
		if dir != root && !strings.HasPrefix(dir, root+"/") {
			continue
		}
		label := path.Base(root)
		if root == s.exec.Home() {
			label = "~"
		}
		out = append(out, Crumb{Name: label, Path: root})
		rel := strings.TrimPrefix(dir, root)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return out
		}
		cur := root
		for _, part := range strings.Split(rel, "/") {
			cur = path.Join(cur, part)
			out = append(out, Crumb{Name: part, Path: cur})
		}
		return out
	}
	return out
}

// verifyDB makes sure the path is a regular file that really is a database.
func (s *Service) verifyDB(p string) (string, error) {
	clean, err := s.ResolvePath(s.rerootIfNeeded(p))
	if err != nil {
		return "", err
	}
	if _, rebased, ok := s.locateDB(p, clean); ok {
		clean = rebased
	}
	return clean, nil
}

// locateDB stats a resolved database path and returns whether it may be used.
// A missing file is acceptable because the first write statement creates it;
// an existing file must be a regular file with the SQLite header. When the
// original path is absolute but the stat fails, the same relative subpath is
// retried under the primary root so recorded paths stay usable across hosts.
func (s *Service) locateDB(p, clean string) (os.FileInfo, string, bool) {
	info, statErr := os.Stat(clean)
	if statErr == nil {
		return info, clean, validateDBFile(clean, info) == nil
	}
	if rebased, ok := s.rebaseExistingFile(p, clean); ok {
		if info, err := os.Stat(rebased); err == nil {
			return info, rebased, true
		}
	}
	if errors.Is(statErr, fs.ErrNotExist) {
		return nil, clean, true
	}
	return nil, "", false
}

// validateDBFile classifies an existing file as usable or not.
func validateDBFile(clean string, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return ErrOutside
	}
	if !hasSQLiteHeader(clean) {
		return ErrBinaryFile
	}
	return nil
}

// rebaseExistingFile retries a missing absolute path against the primary root.
// It returns the rebased path only when that file exists and is a database.
func (s *Service) rebaseExistingFile(p, clean string) (string, bool) {
	if !path.IsAbs(strings.TrimSpace(p)) {
		return "", false
	}
	rebased := s.rerootTo(p, s.Roots()[0])
	if rebased == clean {
		return "", false
	}
	if !hasSQLiteHeader(rebased) {
		return "", false
	}
	info, err := os.Stat(rebased)
	if err != nil || validateDBFile(rebased, info) != nil {
		return "", false
	}
	return rebased, true
}

// Tables lists the user tables and views of a database.
func (s *Service) Tables(p string) ([]Table, error) {
	path, err := s.verifyDB(p)
	if err != nil {
		return nil, err
	}
	script := ".mode list\n" +
		"SELECT name || char(31) || type FROM sqlite_master " +
		"WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' " +
		"ORDER BY type, name;\n"
	ctx, cancel := context.WithTimeout(context.Background(), ReadTimeout)
	defer cancel()
	stdout, stderr, err := s.exec.Exec(ctx, path, script, true)
	if err != nil {
		return nil, cliError(stderr, err)
	}
	var tables []Table
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		name, typ, ok := strings.Cut(line, "\x1f")
		if !ok || name == "" {
			continue
		}
		if len(tables) >= MaxTables {
			break
		}
		tables = append(tables, Table{Name: name, Type: typ, Rows: -1})
	}
	// Row counts are best effort: a broken virtual table must not fail the
	// whole listing.
	if countScript := rowCountScript(tables); countScript != "" {
		ctx2, cancel2 := context.WithTimeout(context.Background(), ReadTimeout)
		defer cancel2()
		out, _, countErr := s.exec.Exec(ctx2, path, countScript, true)
		if countErr == nil {
			applyCounts(tables, out)
		}
	}
	return tables, nil
}

// rowCountScript builds one batched count statement per table.
func rowCountScript(tables []Table) string {
	var b strings.Builder
	for i, t := range tables {
		if i > 0 {
			b.WriteString("UNION ALL\n")
		}
		b.WriteString("SELECT " + strconv.Itoa(i) + " || char(31) || count(*) FROM " +
			quoteIdent(t.Name) + "\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return ".mode list\n" + b.String() + ";\n"
}

// applyCounts writes the count rows back into tables.
func applyCounts(tables []Table, out string) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		idxRaw, cntRaw, ok := strings.Cut(line, "\x1f")
		if !ok {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(idxRaw))
		if err != nil || idx < 0 || idx >= len(tables) {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(cntRaw), 10, 64)
		if err != nil {
			continue
		}
		tables[idx].Rows = n
	}
}

// quoteIdent double-quotes an SQL identifier, escaping embedded quotes.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Schema describes one table: its DDL, columns and indexes.
func (s *Service) Schema(p, table string) (*Schema, error) {
	path, err := s.verifyDB(p)
	if err != nil {
		return nil, err
	}
	table = strings.TrimSpace(table)
	if table == "" {
		return nil, ErrEmptySQL
	}
	lit := quoteLiteral(table)
	sep := " || char(31) || "
	script := ".mode list\n" +
		"SELECT 'ddl'" + sep + "coalesce(sql, '') FROM sqlite_master WHERE name = " + lit + ";\n" +
		"SELECT 'col'" + sep + "cid" + sep + "name" + sep + "type" + sep + "\"notnull\"" + sep +
		"coalesce(dflt_value, '')" + sep + "pk " +
		"FROM pragma_table_info(" + lit + ") ORDER BY cid;\n" +
		"SELECT 'idx'" + sep + "name" + sep + "\"unique\" FROM pragma_index_list(" + lit + ") ORDER BY name;\n" +
		"SELECT 'col2'" + sep + "il.name" + sep + "ii.name FROM pragma_index_list(" + lit + ") AS il, " +
		"pragma_index_info(il.name) AS ii ORDER BY il.name, ii.seqno;\n"
	ctx, cancel := context.WithTimeout(context.Background(), ReadTimeout)
	defer cancel()
	stdout, stderr, err := s.exec.Exec(ctx, path, script, true)
	if err != nil {
		return nil, cliError(stderr, err)
	}
	return parseSchema(table, stdout), nil
}

// parseSchema folds the marker rows of Schema's script into a Schema value.
func parseSchema(table, raw string) *Schema {
	sc := &Schema{Table: table, Columns: []Column{}, Indexes: []Index{}}
	idxPos := map[string]int{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x1f")
		switch f[0] {
		case "ddl":
			if len(f) >= 2 {
				sc.SQL = f[1]
			}
		case "col":
			if len(f) < 7 {
				continue
			}
			cid, _ := strconv.Atoi(f[1])
			notNull, _ := strconv.Atoi(f[4])
			pk, _ := strconv.Atoi(f[6])
			sc.Columns = append(sc.Columns, Column{
				CID:     cid,
				Name:    f[2],
				Type:    f[3],
				NotNull: notNull != 0,
				Default: f[5],
				PK:      pk != 0,
			})
		case "idx":
			if len(f) < 3 {
				continue
			}
			unique, _ := strconv.Atoi(f[2])
			idxPos[f[1]] = len(sc.Indexes)
			sc.Indexes = append(sc.Indexes, Index{Name: f[1], Unique: unique != 0})
		case "col2":
			if len(f) < 3 {
				continue
			}
			if pos, ok := idxPos[f[1]]; ok {
				sc.Indexes[pos].Columns = append(sc.Indexes[pos].Columns, f[2])
			}
		}
	}
	return sc
}

// statements that must never be masked by the read-only probe: anything that
// starts with these keywords is treated as a write.
var writeKeywords = map[string]bool{
	"insert": true, "update": true, "delete": true, "replace": true,
	"drop": true, "alter": true, "vacuum": true, "reindex": true,
	"attach": true, "detach": true, "pragma": true, "analyze": true,
	"begin": true, "commit": true, "rollback": true, "savepoint": true,
	"release": true, "create": true,
}

// IsReadOnly classifies a statement by its leading keyword so the handler can
// decide whether an explicit confirmation is required.
func IsReadOnly(sql string) bool {
	kw := leadingKeyword(sql)
	if kw == "" {
		return false
	}
	if kw == "with" {
		// A CTE can still hide a write; scan the tail for the real verb.
		kw = cteKeyword(sql)
	}
	switch kw {
	case "select", "explain", "values":
		return true
	}
	return !writeKeywords[kw]
}

// leadingKeyword returns the first lower-case word of a statement, skipping
// comments and leading whitespace.
func leadingKeyword(sql string) string {
	s := stripLeadingNoise(sql)
	if s == "" {
		return ""
	}
	end := strings.IndexAny(s, " \t\r\n(")
	if end < 0 {
		return strings.ToLower(s)
	}
	return strings.ToLower(s[:end])
}

// cteKeyword finds the top-level verb after a WITH clause.
func cteKeyword(sql string) string {
	s := stripLeadingNoise(sql)
	depth := 0
	lastClose := -1
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				lastClose = i
			}
		}
	}
	if lastClose < 0 || lastClose+1 >= len(s) {
		return "with"
	}
	return leadingKeyword(s[lastClose+1:])
}

// stripLeadingNoise removes whitespace, line comments and block comments.
func stripLeadingNoise(sql string) string {
	s := strings.TrimSpace(sql)
	for {
		switch {
		case strings.HasPrefix(s, "--"):
			_, rest, ok := strings.Cut(s, "\n")
			if !ok {
				return ""
			}
			s = strings.TrimSpace(rest)
		case strings.HasPrefix(s, "/*"):
			idx := strings.Index(s, "*/")
			if idx < 0 {
				return ""
			}
			s = strings.TrimSpace(s[idx+2:])
		case s != "" && (s[0] == ';' || s[0] == ' ' || s[0] == '\n'):
			s = strings.TrimSpace(s[1:])
		default:
			return s
		}
	}
}

// Query executes one statement. A read-only statement is wrapped in a temporary
// view so the panel can learn the column names before the rows: the header is
// read first, Go builds the per-column row projection from it, and a second run
// prints one framed line per row.
func (s *Service) Query(p, sql string, limit int) (*QueryResult, error) {
	path, err := s.verifyDB(p)
	if err != nil {
		return nil, err
	}
	stmt := strings.TrimSpace(sql)
	if stmt == "" {
		return nil, ErrEmptySQL
	}
	if len(stmt) > MaxSQLLength {
		return nil, fmt.Errorf("%w: statement longer than %d bytes", ErrEmptySQL, MaxSQLLength)
	}
	limit = RowLimit(limit)
	readOnly := IsReadOnly(stmt)
	res := &QueryResult{Columns: []string{}, Rows: [][]any{}}
	start := time.Now()

	timeout := WriteTimeout
	if readOnly {
		timeout = ReadTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if !readOnly {
		done, execErr := s.execWrite(ctx, path, stmt)
		res.DurationMs = time.Since(start).Milliseconds()
		res.Changes = done
		return res, execErr
	}

	// The header must be known before the row projection can be written,
	// because SQLite cannot expand the column list on its own (see rowsSQLFor).
	// Phase one creates the view and reads its columns; phase two re-creates it
	// on a fresh connection and serialises the rows. Both runs stay read-only.
	probe := columnProbeSQL(stmt)
	probeScript := probe + guardSQL() +
		"\n.bail off\nPRAGMA query_only=OFF;\n" + viewDropperSQL()
	out, stderr, err := s.exec.Exec(ctx, path, probeScript, true)
	if err != nil {
		return nil, formatCLIError(stderr, err)
	}
	columns := decodeColumns(mustMarker(out, columnsMarker))
	if len(columns) == 0 {
		return nil, fmt.Errorf("%w: query produced no result set", ErrEmptySQL)
	}
	rowsScript := probe + guardSQL() + rowsSQLFor(columns, limit) +
		"\n.bail off\nPRAGMA query_only=OFF;\n" + viewDropperSQL()
	out, stderr, err = s.exec.Exec(ctx, path, rowsScript, true)
	if err != nil {
		return nil, formatCLIError(stderr, err)
	}
	rows, truncated := decodeRows(extractRows(out, rowsMarker), len(columns), limit)
	res.Columns = columns
	res.Rows = rows
	res.RowCount = len(rows)
	res.Truncated = truncated
	res.DurationMs = time.Since(start).Milliseconds()
	return res, nil
}

// execWrite runs a mutating statement and reports total_changes.
func (s *Service) execWrite(ctx context.Context, path, stmt string) (int64, error) {
	script := ".mode list\n.separator ''\n" + stmt + ";\n" +
		"SELECT 'changes', total_changes();\n"
	stdout, stderr, err := s.exec.Exec(ctx, path, script, false)
	if err != nil {
		return 0, formatCLIError(stderr, err)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if n, ok := strings.CutPrefix(strings.TrimSpace(line), "changes"); ok {
			v, convErr := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
			if convErr == nil {
				return v, nil
			}
		}
	}
	return 0, nil
}

// mustMarker extracts a marker payload, returning "" when absent.
func mustMarker(raw, marker string) string {
	payload, _ := extractMarker(raw, marker)
	return payload
}

// cliError converts a failing CLI run into a domain error.
func cliError(stderr string, err error) error {
	return formatCLIError(stderr, err)
}

// formatCLIError prefers the CLI's own message, which is the useful part.
func formatCLIError(stderr string, err error) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrTimeout
		}
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}
	return errors.New(msg)
}

// isSuspectedDB reports whether a filename looks like a database.
func isSuspectedDB(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range dbExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// hasSQLiteHeader reads the 16 magic bytes of a file.
func hasSQLiteHeader(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(sqliteHeader))
	if _, err := f.Read(buf); err != nil {
		return false
	}
	return string(buf) == sqliteHeader
}

// sortEntries orders the picker: databases, directories, then files.
func sortEntries(entries []DirEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entryLess(entries[j], entries[j-1]); j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

// entryLess is the sort predicate used by sortEntries.
func entryLess(a, b DirEntry) bool {
	ra, rb := entryRank(a), entryRank(b)
	if ra != rb {
		return ra < rb
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}

// entryRank maps an entry to its display group.
func entryRank(e DirEntry) int {
	switch {
	case e.Database:
		return 0
	case e.IsDir:
		return 1
	default:
		return 2
	}
}
