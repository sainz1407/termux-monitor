package handlers

func registerNetworkRoutes(rc *routeContext) {
	// Ping, DNS, TTL URLs
	rc.mux.HandleFunc("/api/network/ping", rc.wrap(HandlePing, true))
	rc.mux.HandleFunc("/api/network/dns", rc.wrap(HandleDNS, true))
	rc.mux.HandleFunc("/api/network/dns/reset", rc.wrap(HandleDNSReset, true))
	rc.mux.HandleFunc("/api/network/ttl", rc.wrap(HandleTTL, true))
	// SoftAP / Hotspot URLs
	rc.mux.HandleFunc("/api/hotspot/status", rc.wrap(HandleHotspotStatus, true))
	rc.mux.HandleFunc("/api/hotspot/control", rc.wrap(HandleHotspotControl, true))
	rc.mux.HandleFunc("/api/hotspot/clients", rc.wrap(HandleHotspotClients, true))
	rc.mux.HandleFunc("/api/hotspot/settings", rc.wrap(HandleHotspotSettings, true))

	// Vnstat Bandwidth Monitoring URLs
	rc.mux.HandleFunc("/api/vnstat/stats", rc.wrap(HandleVnstatStats, true))
	rc.mux.HandleFunc("/api/vnstat/reset", rc.wrap(HandleVnstatReset, true))

	// Speedtest URLs
	rc.mux.HandleFunc("/api/speedtest/start", rc.wrap(HandleSpeedtestStart, true))
	rc.mux.HandleFunc("/api/speedtest/status", rc.wrap(HandleSpeedtestStatus, true))
	rc.mux.HandleFunc("/api/speedtest/stop", rc.wrap(HandleSpeedtestStop, true))
	rc.mux.HandleFunc("/api/speedtest/history", rc.wrap(HandleSpeedtestHistory, true))

	// Cloudflare / Remote Tunnel URLs
	rc.mux.HandleFunc("/api/tunnel/status", rc.wrap(HandleTunnelStatus, true))
	rc.mux.HandleFunc("/api/tunnel/start", rc.wrap(HandleTunnelStart, true))
	rc.mux.HandleFunc("/api/tunnel/stop", rc.wrap(HandleTunnelStop, true))
	rc.mux.HandleFunc("/api/tunnel/upload", rc.wrap(HandleTunnelUploadBinary, true))
}
