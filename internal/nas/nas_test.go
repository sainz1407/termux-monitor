package nas_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"termux-monitor/internal/nas"
)

func TestGetManager(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	mgr := nas.GetManager()
	if mgr == nil {
		t.Fatalf("expected non-nil NAS manager")
	}
}

func TestLoadAndSaveConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	mgr := nas.GetManager()

	cfg := &nas.NASConfig{
		Enabled:      true,
		SharePath:    tempDir,
		Port:         0, // must default to 85
		ReadOnly:     true,
		AuthRequired: true,
		Username:     "admin",
		Password:     "secretpass",
		Protocol:     "", // must default to webdav
	}

	err := mgr.SaveConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error saving NAS config: %v", err)
	}

	loaded, err := mgr.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading NAS config: %v", err)
	}

	if !loaded.Enabled || loaded.Port != 85 || loaded.Protocol != "webdav" {
		t.Errorf("unexpected loaded config values: %+v", loaded)
	}
}

func TestGetLocalIPsAndPrimary(t *testing.T) {
	ips := nas.GetLocalIPs()
	primary := nas.GetPrimaryIP()
	if primary == "" {
		t.Errorf("expected non-empty primary IP")
	}
	_ = ips
}

func TestStartAndStopNAS_WebDAV(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	// Create a dummy file in the shared directory
	testFilePath := filepath.Join(tempDir, "hello.txt")
	if err := os.WriteFile(testFilePath, []byte("Hello WebDAV"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	mgr := nas.GetManager()

	cfg := nas.NASConfig{
		Enabled:      true,
		SharePath:    tempDir,
		Port:         18099,
		ReadOnly:     false,
		AuthRequired: true,
		Username:     "test-user",
		Password:     "test-password",
		Protocol:     "webdav",
	}

	err := mgr.StartNAS(cfg)
	if err != nil {
		t.Fatalf("failed to start WebDAV server: %v", err)
	}
	defer mgr.StopNAS()

	status := mgr.GetStatus()
	if !status.Active {
		t.Errorf("expected NAS Active status to be true")
	}
	if status.Port != 18099 {
		t.Errorf("expected port 18099, got %d", status.Port)
	}
	if len(status.MountURLs) == 0 {
		t.Errorf("expected mount_urls to not be empty")
	}

	baseURL := status.URL

	// 1. Test OPTIONS (WebDAV RFC 4918 returns DAV header)
	reqOpt, _ := http.NewRequest("OPTIONS", baseURL+"/", nil)
	reqOpt.SetBasicAuth("test-user", "test-password")
	respOpt, err := http.DefaultClient.Do(reqOpt)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	respOpt.Body.Close()
	if respOpt.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for OPTIONS, got %d", respOpt.StatusCode)
	}
	if davHdr := respOpt.Header.Get("DAV"); davHdr == "" {
		t.Errorf("expected non-empty DAV header in OPTIONS response")
	}

	// 2. Test PROPFIND
	reqProp, _ := http.NewRequest("PROPFIND", baseURL+"/", nil)
	reqProp.SetBasicAuth("test-user", "test-password")
	reqProp.Header.Set("Depth", "1")
	respProp, err := http.DefaultClient.Do(reqProp)
	if err != nil {
		t.Fatalf("PROPFIND request failed: %v", err)
	}
	respProp.Body.Close()
	if respProp.StatusCode != http.StatusMultiStatus {
		t.Errorf("expected status 207 Multi-Status for PROPFIND, got %d", respProp.StatusCode)
	}

	// 3. Test GET file
	reqGet, _ := http.NewRequest(http.MethodGet, baseURL+"/hello.txt", nil)
	reqGet.SetBasicAuth("test-user", "test-password")
	respGet, err := http.DefaultClient.Do(reqGet)
	if err != nil {
		t.Fatalf("GET file failed: %v", err)
	}
	body, _ := io.ReadAll(respGet.Body)
	respGet.Body.Close()
	if respGet.StatusCode != http.StatusOK || string(body) != "Hello WebDAV" {
		t.Errorf("expected 'Hello WebDAV' with status 200, got status %d body %s", respGet.StatusCode, string(body))
	}

	// 4. Test PUT file
	reqPut, _ := http.NewRequest("PUT", baseURL+"/upload.txt", bytes.NewBufferString("Uploaded via WebDAV"))
	reqPut.SetBasicAuth("test-user", "test-password")
	respPut, err := http.DefaultClient.Do(reqPut)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}
	respPut.Body.Close()
	if respPut.StatusCode != http.StatusCreated && respPut.StatusCode != http.StatusOK && respPut.StatusCode != http.StatusNoContent {
		t.Errorf("expected success for PUT, got status %d", respPut.StatusCode)
	}

	// Verify file was written
	uploadedContent, err := os.ReadFile(filepath.Join(tempDir, "upload.txt"))
	if err != nil || string(uploadedContent) != "Uploaded via WebDAV" {
		t.Errorf("uploaded file content mismatch: %v, content: %s", err, string(uploadedContent))
	}
}

func TestNAS_ReadOnly(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	mgr := nas.GetManager()

	cfg := nas.NASConfig{
		Enabled:      true,
		SharePath:    tempDir,
		Port:         18100,
		ReadOnly:     true,
		AuthRequired: true,
		Username:     "test-user",
		Password:     "test-password",
		Protocol:     "webdav",
	}

	err := mgr.StartNAS(cfg)
	if err != nil {
		t.Fatalf("failed to start NAS server: %v", err)
	}
	defer mgr.StopNAS()

	baseURL := mgr.GetStatus().URL

	// PUT must be rejected with 403 Forbidden
	reqPut, _ := http.NewRequest("PUT", baseURL+"/forbidden.txt", bytes.NewBufferString("test"))
	reqPut.SetBasicAuth("test-user", "test-password")
	respPut, err := http.DefaultClient.Do(reqPut)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}
	respPut.Body.Close()
	if respPut.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for PUT in read-only mode, got %d", respPut.StatusCode)
	}

	// MKCOL must be rejected with 403 Forbidden
	reqMkcol, _ := http.NewRequest("MKCOL", baseURL+"/newdir", nil)
	reqMkcol.SetBasicAuth("test-user", "test-password")
	respMkcol, err := http.DefaultClient.Do(reqMkcol)
	if err != nil {
		t.Fatalf("MKCOL request failed: %v", err)
	}
	respMkcol.Body.Close()
	if respMkcol.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for MKCOL in read-only mode, got %d", respMkcol.StatusCode)
	}

	// DELETE must be rejected with 403 Forbidden
	reqDel, _ := http.NewRequest("DELETE", baseURL+"/forbidden.txt", nil)
	reqDel.SetBasicAuth("test-user", "test-password")
	respDel, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	respDel.Body.Close()
	if respDel.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for DELETE in read-only mode, got %d", respDel.StatusCode)
	}
}

func TestNAS_AuthRequired(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	mgr := nas.GetManager()

	cfg := nas.NASConfig{
		Enabled:      true,
		SharePath:    tempDir,
		Port:         18101,
		ReadOnly:     false,
		AuthRequired: true,
		Username:     "myuser",
		Password:     "mypass",
		Protocol:     "webdav",
	}

	err := mgr.StartNAS(cfg)
	if err != nil {
		t.Fatalf("failed to start NAS server: %v", err)
	}
	defer mgr.StopNAS()

	baseURL := mgr.GetStatus().URL

	// Unauthenticated request should return 401
	respUnauth, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	respUnauth.Body.Close()
	if respUnauth.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", respUnauth.StatusCode)
	}

	// Authenticated request with correct credentials should succeed
	reqAuth, _ := http.NewRequest("OPTIONS", baseURL+"/", nil)
	reqAuth.SetBasicAuth("myuser", "mypass")
	respAuth, err := http.DefaultClient.Do(reqAuth)
	if err != nil {
		t.Fatalf("authenticated request failed: %v", err)
	}
	respAuth.Body.Close()
	if respAuth.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for valid auth, got %d", respAuth.StatusCode)
	}
}

func TestStartAndStopNAS_HTTPFallback(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	testFilePath := filepath.Join(tempDir, "fallback.txt")
	_ = os.WriteFile(testFilePath, []byte("Fallback HTTP"), 0644)

	mgr := nas.GetManager()

	cfg := nas.NASConfig{
		Enabled:      true,
		SharePath:    tempDir,
		Port:         18102,
		ReadOnly:     false,
		AuthRequired: true,
		Username:     "test-user",
		Password:     "test-password",
		Protocol:     "http",
	}

	err := mgr.StartNAS(cfg)
	if err != nil {
		t.Fatalf("failed to start HTTP server: %v", err)
	}
	defer mgr.StopNAS()

	status := mgr.GetStatus()
	req, _ := http.NewRequest(http.MethodGet, status.URL+"/fallback.txt", nil)
	req.SetBasicAuth("test-user", "test-password")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET fallback failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "Fallback HTTP" {
		t.Errorf("expected 'Fallback HTTP' with status 200, got status %d body %s", resp.StatusCode, string(body))
	}
}

func TestNASConfigEdgeCases(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)
	mgr := nas.GetManager()

	// Save nil config
	err := mgr.SaveConfig(nil)
	if err == nil {
		t.Error("expected error saving nil config")
	}

	// Save config with zero values -> auto-defaults
	err = mgr.SaveConfig(&nas.NASConfig{
		Port:      0,
		SharePath: "",
		Protocol:  "",
	})
	if err != nil {
		t.Fatalf("unexpected error saving config: %v", err)
	}

	cfg, _ := mgr.LoadConfig()
	if cfg.Port != 85 || cfg.SharePath != "/sdcard" || cfg.Protocol != "webdav" {
		t.Errorf("expected defaulted fields, got %+v", cfg)
	}
}

func TestGetPrimaryIPAndLocalIPs(t *testing.T) {
	primary := nas.GetPrimaryIP()
	if primary == "" {
		t.Error("expected non-empty primary IP")
	}

	ips := nas.GetLocalIPs()
	t.Logf("Local IPs: %+v", ips)
}

// TestNAS_WebDAVDirectoryListing makes sure a browser opening the share URL
// receives an HTML file list instead of "405 Method Not Allowed".
func TestNAS_WebDAVDirectoryListing(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, "readme.txt"), []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tempDir, "photos"), 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	mgr := nas.GetManager()

	cfg := nas.NASConfig{
		Enabled:      true,
		SharePath:    tempDir,
		Port:         18103,
		ReadOnly:     true,
		AuthRequired: true,
		Username:     "test-user",
		Password:     "test-password",
		Protocol:     "webdav",
	}

	if err := mgr.StartNAS(cfg); err != nil {
		t.Fatalf("failed to start NAS server: %v", err)
	}
	defer mgr.StopNAS()

	baseURL := mgr.GetStatus().URL

	req, _ := http.NewRequest(http.MethodGet, baseURL+"/", nil)
	req.SetBasicAuth("test-user", "test-password")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET directory failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for the directory index, got %d", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Errorf("expected a text/html content type, got %q", contentType)
	}
	page := string(body)
	if !strings.Contains(page, "readme.txt") {
		t.Errorf("expected the file name in the directory index, got %s", page)
	}
	if !strings.Contains(page, "photos/") {
		t.Errorf("expected the directory name in the directory index, got %s", page)
	}

	// A sub directory must be listed as well.
	reqSub, _ := http.NewRequest(http.MethodGet, baseURL+"/photos/", nil)
	reqSub.SetBasicAuth("test-user", "test-password")
	respSub, err := http.DefaultClient.Do(reqSub)
	if err != nil {
		t.Fatalf("GET sub directory failed: %v", err)
	}
	bodySub, _ := io.ReadAll(respSub.Body)
	respSub.Body.Close()
	if respSub.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for the sub directory index, got %d", respSub.StatusCode)
	}
	if !strings.Contains(string(bodySub), "..") {
		t.Errorf("expected a parent link in the sub directory index, got %s", string(bodySub))
	}

	// A file request must still be served by the WebDAV handler.
	reqFile, _ := http.NewRequest(http.MethodGet, baseURL+"/readme.txt", nil)
	reqFile.SetBasicAuth("test-user", "test-password")
	respFile, err := http.DefaultClient.Do(reqFile)
	if err != nil {
		t.Fatalf("GET file failed: %v", err)
	}
	bodyFile, _ := io.ReadAll(respFile.Body)
	respFile.Body.Close()
	if respFile.StatusCode != http.StatusOK || string(bodyFile) != "content" {
		t.Errorf("expected the file content with status 200, got status %d body %s", respFile.StatusCode, string(bodyFile))
	}
}
