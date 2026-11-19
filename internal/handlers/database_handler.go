package handlers

import (
	"errors"
	"net/http"

	"termux-monitor/internal/database"
)

// databaseService is the shared instance used by the Database tab handlers.
// It is stateless: every call funnels into the sqlite3 CLI as the Termux user.
var databaseService = database.NewService()

// databaseErrorStatus maps domain errors to HTTP status codes and messages.
func databaseErrorStatus(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, database.ErrOutside):
		return http.StatusForbidden, "path is outside the allowed roots"
	case errors.Is(err, database.ErrBinaryFile):
		return http.StatusUnprocessableEntity, "file is not an SQLite database"
	case errors.Is(err, database.ErrSQLiteMissing):
		return http.StatusFailedDependency, "sqlite3 is not installed: run `pkg install sqlite`"
	case errors.Is(err, database.ErrEmptySQL):
		return http.StatusBadRequest, "SQL statement is required"
	case errors.Is(err, database.ErrTimeout):
		return http.StatusGatewayTimeout, "sqlite3 did not finish in time"
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

// writeDatabaseError emits the standard error envelope.
func writeDatabaseError(w http.ResponseWriter, err error) {
	code, msg := databaseErrorStatus(err)
	writeStoreJSON(w, code, map[string]any{"error": msg})
}

// HandleDatabaseStatus reports environment capability for the tab header.
func HandleDatabaseStatus(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	writeStoreJSON(w, http.StatusOK, databaseService.Status())
}

// HandleDatabaseFiles lists one directory of the database picker.
func HandleDatabaseFiles(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	path := r.URL.Query().Get("path")
	listing, err := databaseService.ListDir(path)
	if err != nil {
		writeDatabaseError(w, err)
		return
	}
	writeStoreJSON(w, http.StatusOK, listing)
}

// HandleDatabaseTables lists every table and view of one database file.
func HandleDatabaseTables(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	path := r.URL.Query().Get("path")
	tables, err := databaseService.Tables(path)
	if err != nil {
		writeDatabaseError(w, err)
		return
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{
		"path":   path,
		"tables": tables,
	})
}

// HandleDatabaseSchema describes the columns and indexes of one table.
func HandleDatabaseSchema(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	q := r.URL.Query()
	schema, err := databaseService.Schema(q.Get("path"), q.Get("table"))
	if err != nil {
		writeDatabaseError(w, err)
		return
	}
	writeStoreJSON(w, http.StatusOK, schema)
}

// databaseQueryRequest is the body of POST /api/database/query.
type databaseQueryRequest struct {
	Path  string `json:"path"`
	SQL   string `json:"sql"`
	Limit int    `json:"limit"`
	// Confirm must be true for statements that are not read-only.
	Confirm bool `json:"confirm"`
}

// HandleDatabaseQuery executes one SQL statement. Mutating statements require
// an explicit `confirm: true`, so a stray paste cannot rewrite a database.
func HandleDatabaseQuery(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req databaseQueryRequest
	if !decodeCronBody(w, r, &req) {
		return
	}
	if !database.IsReadOnly(req.SQL) && !req.Confirm {
		writeStoreJSON(w, http.StatusConflict, map[string]any{
			"error":     "this statement modifies the database and needs confirmation",
			"needsConfirmation": true,
		})
		return
	}
	result, err := databaseService.Query(req.Path, req.SQL, req.Limit)
	if err != nil {
		writeDatabaseError(w, err)
		return
	}
	writeStoreJSON(w, http.StatusOK, result)
}
