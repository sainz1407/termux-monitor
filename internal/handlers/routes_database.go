package handlers

// registerDatabaseRoutes wires the Database tab endpoints. Every route is
// session-gated because a query can read or rewrite files owned by Termux.
func registerDatabaseRoutes(rc *routeContext) {
	rc.mux.HandleFunc("/api/database/status", rc.wrap(HandleDatabaseStatus, true))
	rc.mux.HandleFunc("/api/database/ls", rc.wrap(HandleDatabaseFiles, true))
	rc.mux.HandleFunc("/api/database/tables", rc.wrap(HandleDatabaseTables, true))
	rc.mux.HandleFunc("/api/database/schema", rc.wrap(HandleDatabaseSchema, true))
	rc.mux.HandleFunc("/api/database/query", rc.wrap(HandleDatabaseQuery, true))
}
