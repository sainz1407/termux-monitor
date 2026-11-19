package nas

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// browserListingHandler wraps the WebDAV handler with a plain HTML directory
// index. The WebDAV handler answers "405 Method Not Allowed" for a directory
// request, which is valid for the protocol but useless when a browser opens
// the share URL, so directory requests are rendered as a file list instead.
// Every other method and every file request is passed to the WebDAV handler
// untouched so that the mount keeps working in a file manager.
type browserListingHandler struct {
	root string
	dav  http.Handler
}

// ServeHTTP renders a directory index for GET and HEAD requests that point at
// a directory, and delegates everything else to the WebDAV handler.
func (h *browserListingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.dav.ServeHTTP(w, r)
		return
	}

	dir, ok := h.resolve(r.URL.Path)
	if !ok {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		h.dav.ServeHTTP(w, r)
		return
	}

	h.renderIndex(w, r, dir)
}

// resolve maps a request path to a location inside the share. It reports false
// when the path escapes the share root.
func (h *browserListingHandler) resolve(urlPath string) (string, bool) {
	cleaned := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	full := filepath.Join(h.root, filepath.FromSlash(cleaned))

	rel, err := filepath.Rel(h.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

// renderIndex writes the HTML file list of a directory.
func (h *browserListingHandler) renderIndex(w http.ResponseWriter, r *http.Request, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "Failed to read the directory", http.StatusInternalServerError)
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	base := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	if base == "." {
		base = "/"
	}

	var body strings.Builder
	body.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">`)
	body.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	body.WriteString(`<title>Termux NAS</title><style>`)
	body.WriteString(`:root{color-scheme:dark light}*{box-sizing:border-box}`)
	body.WriteString(`body{margin:0;padding:24px;background:#0d0d0f;color:#e8e8ea;`)
	body.WriteString(`font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}`)
	body.WriteString(`a{color:#7cc4ff;text-decoration:none}a:hover{text-decoration:underline}`)
	body.WriteString(`h1{font-size:18px;margin:0 0 4px}.path{font-size:13px;color:#9a9aa2;word-break:break-all}`)
	body.WriteString(`table{width:100%;border-collapse:collapse;font-size:14px;margin-top:20px}`)
	body.WriteString(`th,td{text-align:left;padding:8px 10px;border-bottom:1px solid #23232a}`)
	body.WriteString(`th{color:#9a9aa2;font-weight:500;font-size:12px;letter-spacing:.04em}`)
	body.WriteString(`td.size,td.time{color:#9a9aa2;white-space:nowrap}`)
	body.WriteString(`footer{margin-top:18px;font-size:12px;color:#6f6f78}`)
	body.WriteString(`@media (prefers-color-scheme:light){body{background:#f7f7f8;color:#1b1b1f}`)
	body.WriteString(`th,td{border-bottom-color:#e4e4e8}a{color:#0b63c5}`)
	body.WriteString(`.path,td.size,td.time,footer{color:#6b6b74}}`)
	body.WriteString(`</style></head><body>`)

	body.WriteString(`<h1>Termux NAS</h1>`)
	body.WriteString(`<div class="path">` + html.EscapeString(displayPath(h.root, dir)) + `</div>`)
	body.WriteString(`<table><thead><tr><th>Name</th><th>Size</th><th>Modified</th></tr></thead><tbody>`)

	if base != "/" {
		body.WriteString(`<tr><td colspan="3"><a href="` + html.EscapeString(parentURL(base)) + `">..</a></td></tr>`)
	}

	for _, entry := range entries {
		name := entry.Name()
		href := base
		if href == "/" {
			href = ""
		}
		href += "/" + url.PathEscape(name)

		size := "-"
		modified := "-"
		if info, err := entry.Info(); err == nil {
			if !entry.IsDir() {
				size = formatFileSize(info.Size())
			}
			modified = info.ModTime().Format("2006-01-02 15:04:05")
		}
		if entry.IsDir() {
			href += "/"
		}

		body.WriteString(`<tr><td><a href="` + html.EscapeString(href) + `">` + html.EscapeString(name) + `</a></td>`)
		body.WriteString(`<td class="size">` + html.EscapeString(size) + `</td>`)
		body.WriteString(`<td class="time">` + html.EscapeString(modified) + `</td></tr>`)
	}

	body.WriteString(`</tbody></table>`)
	body.WriteString(`<footer>Mount this address in a WebDAV client, or download the files directly from this page.</footer>`)
	body.WriteString(`</body></html>`)

	page := body.String()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprint(len(page)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

// parentURL returns the URL of the parent directory of a request path.
func parentURL(base string) string {
	parent := path.Dir(base)
	if parent == "." || parent == "/" {
		return "/"
	}
	return parent + "/"
}

// displayPath returns the path of a directory relative to the share root.
func displayPath(root string, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return root
	}
	return root + "/" + filepath.ToSlash(rel)
}

// formatFileSize renders a byte count in a human readable form.
func formatFileSize(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(value)/float64(div), []string{"KB", "MB", "GB"}[exp])
}
