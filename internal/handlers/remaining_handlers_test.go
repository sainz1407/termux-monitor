package handlers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"termux-monitor/internal/handlers"
)

func TestHandleBackupExportAndImport(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	// 1. Export GET
	reqExport := httptest.NewRequest("GET", "/api/backup/export", nil)
	rrExport := httptest.NewRecorder()
	handlers.HandleBackupExport(rrExport, reqExport)

	if rrExport.Code != http.StatusOK {
		t.Errorf("expected status 200 for backup export, got %d", rrExport.Code)
	}

	// 2. Import invalid payload POST
	reqImportBad := httptest.NewRequest("POST", "/api/backup/import", bytes.NewBufferString(""))
	rrImportBad := httptest.NewRecorder()
	handlers.HandleBackupImport(rrImportBad, reqImportBad)

	if rrImportBad.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for empty backup import, got %d", rrImportBad.Code)
	}
}

func TestHandleCloudBackupConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	reqGet := httptest.NewRequest("GET", "/api/backup/cloud/config", nil)
	rrGet := httptest.NewRecorder()

	handlers.HandleCloudBackupConfig(rrGet, reqGet)

	if rrGet.Code != http.StatusOK {
		t.Errorf("expected status 200 for GET cloud backup config, got %d", rrGet.Code)
	}
}

// TestHandleStoreMirrorGuardrails covers the two request shapes the mirror
// endpoint must reject before it ever touches sources.list: a write without an
// explicit confirmation and a write naming a mirror that is not curated.
func TestHandleStoreMirrorGuardrails(t *testing.T) {
	// 1. GET always reports the curated choices, even without a Termux install.
	reqGet := httptest.NewRequest("GET", "/api/store/mirror", nil)
	rrGet := httptest.NewRecorder()
	handlers.HandleStoreMirror(rrGet, reqGet)
	if rrGet.Code != http.StatusOK {
		t.Errorf("expected status 200 for GET store mirror, got %d", rrGet.Code)
	}

	// 2. POST without confirmation is refused.
	reqNoConfirm := httptest.NewRequest("POST", "/api/store/mirror", bytes.NewBufferString(`{"id":"cloudflare"}`))
	rrNoConfirm := httptest.NewRecorder()
	handlers.HandleStoreMirror(rrNoConfirm, reqNoConfirm)
	if rrNoConfirm.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for unconfirmed mirror write, got %d", rrNoConfirm.Code)
	}

	// 3. POST with an unknown mirror id is refused.
	reqUnknown := httptest.NewRequest("POST", "/api/store/mirror", bytes.NewBufferString(`{"id":"nope","confirmed":true}`))
	rrUnknown := httptest.NewRecorder()
	handlers.HandleStoreMirror(rrUnknown, reqUnknown)
	if rrUnknown.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for unknown mirror, got %d", rrUnknown.Code)
	}
}
