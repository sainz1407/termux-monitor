package handlers

import (
	"errors"
	"net/http"
	"os"

	"termux-monitor/internal/config"
	"termux-monitor/internal/logger"
	"termux-monitor/internal/website"
)

// websiteService is the process-wide Website domain instance. Sites live beside
// the other persistent data files so they survive a module reinstall, while
// their PID and log files live in the Termux runtime directory.
var websiteService = website.NewService(
	website.NewRegistry(config.GetPersistentFilePath("sites.json")),
)

// websiteErrorStatus maps the domain sentinels to HTTP status codes. Anything
// unrecognised is a 500 because the request itself was well formed.
func websiteErrorStatus(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, website.ErrUnknownSite):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, website.ErrNameRequired),
		errors.Is(err, website.ErrBadEngine),
		errors.Is(err, website.ErrBadPort),
		errors.Is(err, website.ErrPortInUse),
		errors.Is(err, website.ErrPathRequired),
		errors.Is(err, website.ErrPathMissing),
		errors.Is(err, website.ErrPathFile),
		errors.Is(err, website.ErrPathDir),
		errors.Is(err, website.ErrEngineAbsent):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

// writeWebsiteError renders a domain error as a JSON body with its status.
func writeWebsiteError(w http.ResponseWriter, err error) {
	status, msg := websiteErrorStatus(err)
	writeStoreJSON(w, status, map[string]any{"error": msg})
}

// hostname returns the device hostname for building site URLs, falling back to
// an empty string so the domain layer substitutes loopback.
func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

// decodeWebsiteBody decodes a JSON body into v, reporting a 400 on malformed
// input.
func decodeWebsiteBody(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeCronBody(w, r, v)
}

// HandleWebsiteList returns every site with its live runtime state.
// GET /api/website/list
func HandleWebsiteList(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	available, reason := websiteService.Available()
	writeStoreJSON(w, http.StatusOK, map[string]any{
		"available": available,
		"reason":    reason,
		"sites":     websiteService.List(hostname()),
		"engines":   website.Engines,
	})
}

// HandleWebsiteAdd registers a new site. The engine is not started here: the UI
// starts it explicitly so a typo in the path cannot leave a stray process.
// POST /api/website/add  {"name","engine","path","port"}
func HandleWebsiteAdd(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Name   string `json:"name"`
		Engine string `json:"engine"`
		Path   string `json:"path"`
		Port   int    `json:"port"`
	}
	if !decodeWebsiteBody(w, r, &req) {
		return
	}
	site, err := websiteService.Add(req.Name, req.Engine, req.Path, req.Port)
	if err != nil {
		writeWebsiteError(w, err)
		return
	}
	logger.Get().Infof("website", "site added: %s (%s) on port %d", site.Name, site.Engine, site.Port)
	writeStoreJSON(w, http.StatusOK, map[string]any{"site": site})
}

// HandleWebsiteStart launches a site's engine.
// POST /api/website/start  {"id"}
func HandleWebsiteStart(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	id, ok := websiteIDFromBody(w, r)
	if !ok {
		return
	}
	if err := websiteService.Start(id); err != nil {
		writeWebsiteError(w, err)
		return
	}
	logger.Get().Infof("website", "site started: %s", id)
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleWebsiteStop terminates a site's engine.
// POST /api/website/stop  {"id"}
func HandleWebsiteStop(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	id, ok := websiteIDFromBody(w, r)
	if !ok {
		return
	}
	if err := websiteService.Stop(id); err != nil {
		writeWebsiteError(w, err)
		return
	}
	logger.Get().Infof("website", "site stopped: %s", id)
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleWebsiteRestart stops and starts a site again.
// POST /api/website/restart  {"id"}
func HandleWebsiteRestart(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	id, ok := websiteIDFromBody(w, r)
	if !ok {
		return
	}
	if err := websiteService.Restart(id); err != nil {
		writeWebsiteError(w, err)
		return
	}
	logger.Get().Infof("website", "site restarted: %s", id)
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleWebsiteDelete removes a site, stopping it first.
// POST /api/website/delete  {"id"}
func HandleWebsiteDelete(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	id, ok := websiteIDFromBody(w, r)
	if !ok {
		return
	}
	if err := websiteService.Delete(id); err != nil {
		writeWebsiteError(w, err)
		return
	}
	logger.Get().Infof("website", "site deleted: %s", id)
	writeStoreJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleWebsiteLogs returns the tail of a site's captured output.
// GET /api/website/logs?id=xxxx
func HandleWebsiteLogs(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	text, err := websiteService.Logs(id)
	if err != nil {
		writeWebsiteError(w, err)
		return
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{
		"logs":    text,
		"running": websiteService.PID(id) > 0,
	})
}

// HandleWebsiteProbe reports the engine inventory, used by the Add dialog to
// warn before a site is created with a missing engine.
// GET /api/website/probe
func HandleWebsiteProbe(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	type engineInfo struct {
		Engine    string `json:"engine"`
		Bin       string `json:"bin"`
		Installed bool   `json:"installed"`
	}
	infos := make([]engineInfo, 0, len(website.Engines))
	for _, engine := range website.Engines {
		infos = append(infos, engineInfo{
			Engine:    engine,
			Bin:       website.EngineBin(engine),
			Installed: websiteService.EngineInstalled(engine),
		})
	}
	writeStoreJSON(w, http.StatusOK, map[string]any{"engines": infos})
}

// websiteIDFromBody decodes a {id} body and validates its shape, reporting a
// 400 on malformed input.
func websiteIDFromBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeWebsiteBody(w, r, &req) {
		return "", false
	}
	if req.ID == "" {
		writeStoreJSON(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return "", false
	}
	return req.ID, true
}
