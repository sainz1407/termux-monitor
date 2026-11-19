package store

// Item is one installable Termux package shown in the Store tab.
type Item struct {
	// ID is the stable catalog identifier used in API routes.
	ID string
	// Name is the display name.
	Name string
	// Desc is a short one-line description.
	Desc string
	// Bin is the binary used for presence and version probes.
	Bin string
	// Check overrides the presence probe when it differs from Bin.
	Check string
	// VersionCmd overrides the version probe; empty derives from Bin.
	VersionCmd string
	// Pkg is the Termux package name passed to `pkg install`.
	Pkg string
	// Conf is a static config path shown on the card (display only).
	Conf string
	// Group is the UI grouping category.
	Group string
}

// Groups used by the frontend for section headers.
const (
	GroupRuntime  = "runtime"
	GroupDatabase = "database"
	GroupWeb      = "web"
	GroupTools    = "tools"
	GroupNetwork  = "network"
)

// Catalog is the Termux-only package catalog. Items whose OLP definition had
// no `install.termux` command are omitted: showing a package that cannot be
// installed here would be dishonest.
var Catalog = []Item{
	// Runtime & languages
	{ID: "nodejs", Name: "Node.js", Desc: "JavaScript runtime", Bin: "node", Pkg: "nodejs", Group: GroupRuntime},
	{ID: "python", Name: "Python", Desc: "Python interpreter", Bin: "python",
		VersionCmd: "(python --version 2>&1 || python3 --version 2>&1) | head -n 1",
		Pkg:        "python", Group: GroupRuntime},
	{ID: "php", Name: "PHP", Desc: "PHP interpreter", Bin: "php", Pkg: "php", Group: GroupRuntime},
	{ID: "golang", Name: "Go", Desc: "Go programming language", Bin: "go",
		VersionCmd: "go version 2>&1 | head -n 1", Pkg: "golang", Group: GroupRuntime},
	{ID: "composer", Name: "Composer", Desc: "PHP package manager", Bin: "composer",
		Pkg: "composer", Group: GroupRuntime},
	{ID: "bun", Name: "Bun", Desc: "Fast JavaScript runtime", Bin: "bun",
		Check: `command -v bun >/dev/null 2>&1 || [ -f "$HOME/.bun/bin/bun" ]`,
		Group: GroupRuntime},
	{ID: "pm2", Name: "PM2", Desc: "Node.js process manager", Bin: "pm2",
		Group: GroupRuntime},

	// Databases
	{ID: "mysql", Name: "MySQL (MariaDB)", Desc: "MySQL-compatible database server",
		Bin: "mysqld", Pkg: "mariadb", Conf: "$PREFIX/etc/mysql/my.cnf", Group: GroupDatabase},
	{ID: "postgres", Name: "PostgreSQL", Desc: "PostgreSQL database server",
		Bin: "postgres", Pkg: "postgresql", Group: GroupDatabase},
	{ID: "redis", Name: "Redis", Desc: "In-memory database & cache",
		Bin: "redis-server", Pkg: "redis", Group: GroupDatabase},
	{ID: "sqlite", Name: "SQLite", Desc: "Lightweight serverless database",
		Bin: "sqlite3", Pkg: "sqlite", Group: GroupDatabase},

	// Web & proxy
	{ID: "nginx", Name: "Nginx", Desc: "Web server & reverse proxy", Bin: "nginx",
		VersionCmd: "nginx -v 2>&1 | head -n 1",
		Pkg:        "nginx", Conf: "$PREFIX/etc/nginx/nginx.conf", Group: GroupWeb},
	{ID: "cloudflared", Name: "Cloudflared", Desc: "Cloudflare Tunnel client",
		Bin: "cloudflared", Pkg: "cloudflared", Conf: "~/.cloudflared/config.yml", Group: GroupWeb},

	// Tools
	{ID: "git", Name: "Git", Desc: "Version control system", Bin: "git",
		Pkg: "git", Conf: "~/.gitconfig", Group: GroupTools},
	{ID: "tmux", Name: "Tmux", Desc: "Terminal multiplexer", Bin: "tmux",
		VersionCmd: "tmux -V 2>&1 | head -n 1", Pkg: "tmux", Conf: "~/.tmux.conf", Group: GroupTools},
	{ID: "ffmpeg", Name: "FFmpeg", Desc: "Video & audio processing", Bin: "ffmpeg",
		VersionCmd: "ffmpeg -version 2>&1 | head -n 1", Pkg: "ffmpeg", Group: GroupTools},
	{ID: "rsync", Name: "Rsync", Desc: "File sync & backup", Bin: "rsync",
		Pkg: "rsync", Group: GroupTools},
	{ID: "zip", Name: "Zip", Desc: "ZIP archive creation", Bin: "zip",
		VersionCmd: "zip -v 2>&1 | head -n 2 | tail -n 1", Pkg: "zip", Group: GroupTools},
	{ID: "unzip", Name: "Unzip", Desc: "ZIP archive extraction", Bin: "unzip",
		Pkg: "unzip", Group: GroupTools},
	{ID: "yt-dlp", Name: "yt-dlp", Desc: "Video & audio downloader", Bin: "yt-dlp",
		Pkg: "yt-dlp", Group: GroupTools},

	// Network & system
	{ID: "openssh", Name: "OpenSSH", Desc: "SSH server & client (port 8022)", Bin: "sshd",
		VersionCmd: "ssh -V 2>&1 | head -n 1", Pkg: "openssh",
		Conf: "~/.ssh/authorized_keys", Group: GroupNetwork},
	{ID: "cronie", Name: "Cronie", Desc: "Cron daemon for scheduled jobs", Bin: "crond",
		Check: `command -v crond >/dev/null 2>&1 && command -v crontab >/dev/null 2>&1`,
		Pkg:   "cronie", Group: GroupNetwork},
	{ID: "termux-api", Name: "Termux:API", Desc: "Phone sensors & notifications",
		Bin: "termux-battery-status", Pkg: "termux-api", Group: GroupNetwork},
}

// ItemByID finds a catalog item by its identifier.
func ItemByID(id string) (Item, bool) {
	for _, it := range Catalog {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// checkCommand returns the presence probe for an item.
func (it Item) checkCommand() string {
	if it.Check != "" {
		return it.Check
	}
	if it.Bin != "" {
		return "command -v " + it.Bin + " >/dev/null 2>&1"
	}
	return "exit 1"
}

// versionCommand returns the version probe, or "" when the item has none.
func (it Item) versionCommand() string {
	if it.VersionCmd != "" {
		return it.VersionCmd
	}
	if it.Bin != "" {
		return it.Bin + " --version 2>&1 | head -n 1"
	}
	return ""
}

// installCommand returns the shell command that installs the item. Custom
// (non-pkg) items such as Bun carry their own one-liner.
func (it Item) installCommand() string {
	switch it.ID {
	case "bun":
		return "curl -fsSL https://bun.sh/install | bash"
	case "pm2":
		return "npm install -g pm2"
	}
	return "pkg install -y " + it.Pkg
}
