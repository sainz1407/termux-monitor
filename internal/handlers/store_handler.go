package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"termux-monitor/internal/logger"
	"termux-monitor/internal/store"
)

// storeExecutor is the process-wide run registry for the Store tab.
var storeExecutor = store.NewExecutor()

// writeStoreJSON writes a JSON payload with the given status code.
func writeStoreJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

// HandleStoreStatus reports whether the Termux environment is usable.
// GET /api/store/status
func HandleStoreStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	reason := store.AvailableReason()
	uid := store.ResolveUID()
	// Level 2: report which curated mirror the active sources.list uses. This
	// reads one local file — no network probe, no periodic health check, so
	// opening the tab never waits on a remote repo.
	read := store.ReadSourcesList()
	writeStoreJSON(w, http.StatusOK, map[string]any{
		"available": reason == "",
		"reason":    reason,
		"uid":       uid,
		"home":      store.TermuxHome,
		"prefix":    store.TermuxPrefix,
		"mirrorId":  read.ActiveID,
		"mirrorList": store.SourcesListPath(),
	})
}

// HandleStoreList returns the catalog with presence and version probes.
// GET /api/store/list
func HandleStoreList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !store.Available() {
		writeStoreJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    store.AvailableReason(),
			"items":     []any{},
		})
		return
	}

	type itemView struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Desc      string `json:"desc"`
		Group     string `json:"group"`
		Conf      string `json:"conf"`
		Installed bool   `json:"installed"`
		Version   string `json:"version"`
		Installing bool  `json:"installing"`
	}

	items := make([]itemView, 0, len(store.Catalog))
	for _, it := range store.Catalog {
		view := itemView{
			ID:    it.ID,
			Name:  it.Name,
			Desc:  it.Desc,
			Group: it.Group,
			Conf:  it.Conf,
		}
		view.Installed = store.IsInstalled(it)
		if view.Installed {
			view.Version = store.Version(it)
		}
		if run := storeExecutor.RunFor(it.ID); run != nil {
			view.Installing = run.IsRunning()
		}
		items = append(items, view)
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"items":     items,
	})
}

// HandleStoreMirror reads or replaces the active Termux apt sources.list.
//
//	GET  /api/store/mirror  -> current content + detected active mirror
//	POST /api/store/mirror  {"id": "cloudflare"} -> rewrite and back up
//
// Only the single active sources.list is touched: Termux keeps every repository
// in that file, so switching there is equivalent to running termux-change-repo.
func HandleStoreMirror(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleStoreMirrorGet(w, r)
	case http.MethodPost:
		handleStoreMirrorSet(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleStoreMirrorGet reports the active file and the curated choices.
func handleStoreMirrorGet(w http.ResponseWriter, _ *http.Request) {
	type mirrorView struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Host        string   `json:"host"`
		URLs        []string `json:"urls"`
	}
	choices := make([]mirrorView, 0, len(store.CuratedMirrors))
	for _, m := range store.CuratedMirrors {
		choices = append(choices, mirrorView{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			Host:        m.Host,
			URLs:        m.URLs,
		})
	}

	read := store.ReadSourcesList()
	writeStoreJSON(w, http.StatusOK, map[string]any{
		"available": store.Available(),
		"reason":    store.AvailableReason(),
		"path":      store.SourcesListPath(),
		"exists":    read.Exists,
		"content":   read.Content,
		"activeId":  read.ActiveID,
		"readError": read.Reason,
		"backup":    store.LatestBackup(),
		"mirrors":   choices,
	})
}

// handleStoreMirrorSet rewrites sources.list to the requested mirror. The
// request must carry an explicit confirmation flag: the front end only sets it
// after the user accepts the change in a dialog.
func handleStoreMirrorSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		Confirmed bool   `json:"confirmed"`
		// Confirm is accepted as an alias so either spelling works.
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	// Request-shape validation comes first: a malformed or unconfirmed request
	// is rejected the same way no matter what the host environment looks like,
	// and the caller gets the specific reason rather than a generic 503.
	if !req.Confirmed && !req.Confirm {
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "confirmation required before rewriting sources.list"})
		return
	}
	// The mirror is resolved by ID only; the caller can never supply a raw URL,
	// so an unknown ID is rejected rather than interpolated into the file.
	if _, ok := store.MirrorByID(strings.TrimSpace(req.ID)); !ok {
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown mirror"})
		return
	}
	// Only now does the host matter: rewriting sources.list is impossible
	// without a Termux installation to rewrite it for.
	if !store.Available() {
		writeStoreJSON(w, http.StatusServiceUnavailable, map[string]any{"error": store.AvailableReason()})
		return
	}

	res, err := store.WriteMirror(strings.TrimSpace(req.ID))
	if err != nil {
		writeStoreJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	logger.Get().Infof("store", "sources.list rewritten to mirror %s (backup: %s)", req.ID, res.Backup)

	read := store.ReadSourcesList()
	writeStoreJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"backup":   res.Backup,
		"content":  read.Content,
		"activeId": read.ActiveID,
	})
}

// HandleStoreInstallStart launches a package installation.
// POST /api/store/install  {"id": "nodejs"}
func HandleStoreInstallStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !store.Available() {
		writeStoreJSON(w, http.StatusServiceUnavailable, map[string]any{"error": store.AvailableReason()})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	item, ok := store.ItemByID(strings.TrimSpace(req.ID))
	if !ok {
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown package"})
		return
	}
	if !storeExecutor.LaunchInstall(item) {
		writeStoreJSON(w, http.StatusConflict, map[string]any{"error": "installation already running"})
		return
	}
	logger.Get().Infof("store", "install started: %s", item.ID)
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleStoreInstallLog streams the retained log of an installation run.
// GET /api/store/install/log?id=nodejs
func HandleStoreInstallLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if _, ok := store.ItemByID(id); !ok {
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown package"})
		return
	}
	writeStoreJSON(w, http.StatusOK, store.ReadLog(storeExecutor.RunFor(id)))
}

// HandleStoreOpStart launches a package-manager operation.
// POST /api/store/op  {"op": "update"|"upgrade"|"purge"}
func HandleStoreOpStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !store.Available() {
		writeStoreJSON(w, http.StatusServiceUnavailable, map[string]any{"error": store.AvailableReason()})
		return
	}
	var req struct {
		Op string `json:"op"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	var launched bool
	op := strings.TrimSpace(req.Op)
	switch op {
	case "update":
		launched = storeExecutor.LaunchUpdate()
	case "upgrade":
		launched = storeExecutor.LaunchUpgrade()
	case "purge":
		launched = storeExecutor.LaunchPurge()
	default:
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown operation"})
		return
	}
	if !launched {
		writeStoreJSON(w, http.StatusConflict, map[string]any{"error": "operation already running"})
		return
	}
	logger.Get().Infof("store", "operation started: %s", op)
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleStoreOpLog streams the retained log of a manager operation.
// GET /api/store/op/log?op=update
func HandleStoreOpLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	op := strings.TrimSpace(r.URL.Query().Get("op"))
	switch op {
	case "update", "upgrade", "purge":
	default:
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown operation"})
		return
	}
	writeStoreJSON(w, http.StatusOK, store.ReadLog(storeExecutor.OpFor(op)))
}
