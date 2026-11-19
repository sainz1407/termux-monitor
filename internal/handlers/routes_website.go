package handlers

// registerWebsiteRoutes wires the Website tab endpoints. Every route is
// session-gated: starting a process as the Termux user is a privileged action.
func registerWebsiteRoutes(rc *routeContext) {
	rc.mux.HandleFunc("/api/website/list", rc.wrap(HandleWebsiteList, true))
	rc.mux.HandleFunc("/api/website/probe", rc.wrap(HandleWebsiteProbe, true))
	rc.mux.HandleFunc("/api/website/add", rc.wrap(HandleWebsiteAdd, true))
	rc.mux.HandleFunc("/api/website/start", rc.wrap(HandleWebsiteStart, true))
	rc.mux.HandleFunc("/api/website/stop", rc.wrap(HandleWebsiteStop, true))
	rc.mux.HandleFunc("/api/website/restart", rc.wrap(HandleWebsiteRestart, true))
	rc.mux.HandleFunc("/api/website/delete", rc.wrap(HandleWebsiteDelete, true))
	rc.mux.HandleFunc("/api/website/logs", rc.wrap(HandleWebsiteLogs, true))
}
