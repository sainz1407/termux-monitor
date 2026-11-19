// Package website implements the static/dynamic site manager for the Website
// tab. Every site is a long-lived process started as the Termux application
// user, because the server itself runs as root from a Magisk module and node,
// php and python only exist inside Termux.
package website

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Supported engines. The panel deliberately drops the OLP `go run .` engine:
// compiling a Go program on every start is too heavy for a phone and a
// prebuilt binary is better served by the static engine.
const (
	EngineNode   = "node"
	EnginePHP    = "php"
	EngineStatic = "static"
)

// Engines lists the supported values in display order.
var Engines = []string{EngineNode, EnginePHP, EngineStatic}

// Validation bounds. Ports below 1024 need root and would shadow system
// services, so the panel refuses them.
const (
	MinPort = 1024
	MaxPort = 65535
)

// Errors surfaced to the API. The handler maps them to 400 responses, matching
// the OLP contract where thrown errors become `{error}` bodies.
var (
	ErrNameRequired = errors.New("Name is required")
	ErrBadEngine    = errors.New("Engine must be node, php or static")
	ErrBadPort      = errors.New("Port must be an integer between 1024 and 65535")
	ErrPortInUse    = errors.New("Port is already used by another site")
	ErrPathRequired = errors.New("Path is required")
	ErrPathMissing  = errors.New("Path does not exist")
	ErrPathFile     = errors.New("Node sites must point to a file")
	ErrPathDir      = errors.New("PHP and static sites must point to a directory")
	ErrUnknownSite  = errors.New("Unknown site")
	ErrEngineAbsent = errors.New("Engine binary is not installed. Install it via the Store menu.")
	ErrStartFailed  = errors.New("Engine did not start. Check the site log for details.")
)

// TermuxHomePath is duplicated here as a plain constant so this package does
// not import internal/store just for a path string.
const TermuxHomePath = "/data/data/com.termux/files/home"

// siteIDPattern matches the ids this package generates. The alphabet stays
// inside [a-z0-9] so the id is safe to interpolate into a PID file name and
// into a shell command without quoting.
var siteIDPattern = regexp.MustCompile(`^[a-z0-9]{8}$`)

// randomHex returns n random bytes as lowercase hex. A failure of the system
// entropy source is unrecoverable, so it panics rather than handing out an
// id that could collide.
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("website: random source unavailable: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// Site is one managed website. The JSON tags are the wire contract with
// frontend/src/types/website.ts.
type Site struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Engine    string `json:"engine"`
	Path      string `json:"path"`
	Port      int    `json:"port"`
	CreatedAt int64  `json:"createdAt"`
}

// SiteView is a Site enriched with live runtime state. These fields are never
// persisted: they are recomputed from the PID file on every listing.
type SiteView struct {
	Site
	Running       bool   `json:"running"`
	PID           int    `json:"pid"`
	URL           string `json:"url"`
	UptimeMs      int64  `json:"uptimeMs"`
	EngineOK      bool   `json:"engineInstalled"`
	EngineVersion string `json:"engineVersion"`
}

// registryFile is the on-disk shape. Wrapping the array in an object keeps the
// file extensible and matches the cron bookkeeping convention.
type registryFile struct {
	Sites []Site `json:"sites"`
}

// Registry is the mutex-guarded site store. The file lives beside the other
// persistent data files so it survives a module reinstall.
type Registry struct {
	mu     sync.Mutex
	path   string
	sites  []Site
	loaded bool
}

// NewRegistry opens (or lazily reads) the registry at path. A missing, empty or
// corrupt file simply means an empty list: managing sites must never break the
// Website tab.
func NewRegistry(path string) *Registry {
	return &Registry{path: path}
}

// load reads the backing file once. Callers must hold the mutex.
func (r *Registry) load() {
	if r.loaded {
		return
	}
	r.loaded = true
	if r.path == "" {
		return
	}
	data, err := os.ReadFile(r.path)
	if err != nil || len(data) == 0 {
		return
	}
	// Accept both the current object shape and a bare array for robustness
	// against a hand-edited file.
	var file registryFile
	if err := json.Unmarshal(data, &file); err == nil {
		r.sites = file.Sites
		return
	}
	var bare []Site
	if err := json.Unmarshal(data, &bare); err == nil {
		r.sites = bare
	}
}

// save writes the registry with owner-only permissions. Failures are reported
// because a lost write would silently drop a site definition.
func (r *Registry) save() error {
	if r.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(registryFile{Sites: r.sites}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0o600)
}

// List returns every site in insertion order.
func (r *Registry) List() []Site {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	out := make([]Site, len(r.sites))
	copy(out, r.sites)
	return out
}

// Find returns the site with the given id, or false.
func (r *Registry) Find(id string) (Site, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	for _, s := range r.sites {
		if s.ID == id {
			return s, true
		}
	}
	return Site{}, false
}

// portTaken reports whether another site already claims port, excluding the
// site with the given id (used when updating).
func (r *Registry) portTaken(port int, excludeID string) bool {
	for _, s := range r.sites {
		if s.ID != excludeID && s.Port == port {
			return true
		}
	}
	return false
}

// Add validates and appends a new site.
func (r *Registry) Add(name, engine, path string, port int) (Site, error) {
	clean, err := validate(name, engine, path, port)
	if err != nil {
		return Site{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	if r.portTaken(clean.Port, "") {
		return Site{}, ErrPortInUse
	}
	clean.ID = randomHex(4)
	clean.CreatedAt = time.Now().UnixMilli()
	r.sites = append(r.sites, clean)
	if err := r.save(); err != nil {
		// Roll the in-memory append back so memory and disk stay consistent.
		r.sites = r.sites[:len(r.sites)-1]
		return Site{}, err
	}
	return clean, nil
}

// Delete removes a site by id and persists the change. Unknown ids are a no-op
// success so a double tap from two browser tabs cannot fail.
func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.load()
	kept := make([]Site, 0, len(r.sites))
	for _, s := range r.sites {
		if s.ID == id {
			continue
		}
		kept = append(kept, s)
	}
	r.sites = kept
	return r.save()
}

// validate normalises the incoming fields and enforces every invariant the UI
// also checks, because the API must not trust the browser.
func validate(name, engine, path string, port int) (Site, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Site{}, ErrNameRequired
	}
	engine = strings.ToLower(strings.TrimSpace(engine))
	switch engine {
	case EngineNode, EnginePHP, EngineStatic:
	default:
		return Site{}, ErrBadEngine
	}
	if port < MinPort || port > MaxPort {
		return Site{}, ErrBadPort
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return Site{}, ErrPathRequired
	}
	if !filepath.IsAbs(path) {
		// Relative paths are resolved against the Termux home directory, which
		// is always the working directory of a Termux command.
		path = TermuxHomePath + "/" + path
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return Site{}, ErrPathMissing
	}
	if engine == EngineNode {
		// `node` needs a script file, not a directory.
		if info.IsDir() {
			return Site{}, ErrPathFile
		}
	} else if !info.IsDir() {
		// php -S and python3 -m http.server both serve a directory.
		return Site{}, ErrPathDir
	}

	return Site{
		Name:   name,
		Engine: engine,
		Path:   path,
		Port:   port,
	}, nil
}

// EngineCommand returns the long-running command line for a site. The site's
// directory becomes the working directory, matching the OLP behaviour where
// `node app.js` runs next to app.js.
//
// Only validated values reach this function, and every interpolated value is
// shell-quoted, so the returned string is safe to hand to `sh -c`.
func EngineCommand(site Site) string {
	switch site.Engine {
	case EngineNode:
		return "exec node " + shellQuote(site.Path)
	case EnginePHP:
		return "exec php -S 127.0.0.1:" + strconv.Itoa(site.Port) + " -t " + shellQuote(site.Path)
	default: // EngineStatic
		return "exec python3 -m http.server " + strconv.Itoa(site.Port) +
			" --directory " + shellQuote(site.Path)
	}
}

// WorkingDir returns the directory a site's process should start in.
func WorkingDir(site Site) string {
	if site.Engine == EngineNode {
		return filepath.Dir(site.Path)
	}
	return site.Path
}

// EngineBin returns the binary a site's engine needs.
func EngineBin(engine string) string {
	switch engine {
	case EngineNode:
		return "node"
	case EnginePHP:
		return "php"
	default:
		return "python3"
	}
}

// shellQuote wraps s in single quotes, escaping embedded quotes for POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// validID reports whether id has the exact shape this package generates, which
// keeps a hostile id from escaping the runtime directory.
func validID(id string) bool {
	return siteIDPattern.MatchString(id)
}
