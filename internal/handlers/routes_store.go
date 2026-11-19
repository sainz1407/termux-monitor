package handlers

// registerStoreRoutes wires the Termux package Store endpoints. Every route
// is session-gated: installing packages is a privileged action.
func registerStoreRoutes(rc *routeContext) {
	rc.mux.HandleFunc("/api/store/status", rc.wrap(HandleStoreStatus, true))
	rc.mux.HandleFunc("/api/store/list", rc.wrap(HandleStoreList, true))
	rc.mux.HandleFunc("/api/store/install", rc.wrap(HandleStoreInstallStart, true))
	rc.mux.HandleFunc("/api/store/install/log", rc.wrap(HandleStoreInstallLog, true))
	rc.mux.HandleFunc("/api/store/op", rc.wrap(HandleStoreOpStart, true))
	rc.mux.HandleFunc("/api/store/op/log", rc.wrap(HandleStoreOpLog, true))
	rc.mux.HandleFunc("/api/store/mirror", rc.wrap(HandleStoreMirror, true))
}
