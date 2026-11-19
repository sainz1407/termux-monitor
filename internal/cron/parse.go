package cron

import (
	"strings"
)

// TagPrefix marks a crontab line as owned by this panel. OpenLocalPanel uses
// "# panel:"; Termux Monitor uses its own namespace so both panels can manage
// their own jobs inside a single crontab without fighting over each other.
const TagPrefix = "# tmon:"

// tagRe matches the tag produced by JobLine. The id is a hex string (we emit 8
// characters) and the remainder is the human name.
//
// It is implemented as a hand-written scan rather than a regexp because the
// per-line cost dominates crontab reads and the format is trivial.

// Job is one panel-tagged crontab entry.
type Job struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`
}

// splitTag locates the trailing "# tmon:<id>:<name>" marker. It returns the
// id, the name and the index where the marker starts. ok is false when the
// line carries no marker or the marker has no id.
func splitTag(text string) (id, name string, at int, ok bool) {
	idx := strings.Index(text, TagPrefix)
	if idx < 0 {
		return "", "", 0, false
	}
	rest := text[idx+len(TagPrefix):]
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 {
		// An empty id or a missing separator means the line is not ours.
		return "", "", 0, false
	}
	id = rest[:colon]
	if !isHex(id) {
		return "", "", 0, false
	}
	return id, rest[colon+1:], idx, true
}

// isHex reports whether every byte is a lowercase hexadecimal digit.
func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return len(s) > 0
}

// ParseLine parses one crontab line into a Job when it carries our tag and a
// valid 5-field prefix, else a nil Job. A leading "#" (with any indentation)
// means the job is disabled.
func ParseLine(line string) *Job {
	enabled := true
	text := line
	if strings.HasPrefix(strings.TrimLeft(text, " \t"), "#") {
		enabled = false
		text = strings.Replace(text, "#", "", 1)
	}
	id, name, at, ok := splitTag(text)
	if !ok {
		return nil
	}
	head := strings.Fields(strings.TrimSpace(text[:at]))
	if len(head) < 6 {
		return nil
	}
	return &Job{
		ID:       id,
		Name:     strings.TrimSpace(name),
		Schedule: strings.Join(head[:5], " "),
		Command:  strings.Join(head[5:], " "),
		Enabled:  enabled,
	}
}

// JobLine renders a job back to its tagged crontab line. Name newlines and
// hashes are replaced with spaces so a crafted name cannot inject a second
// crontab line or a fake tag.
func JobLine(id, name, schedule, command string) string {
	safe := strings.NewReplacer("\r", " ", "\n", " ", "#", " ").Replace(name)
	return schedule + " " + command + " " + TagPrefix + id + ":" + safe
}

// SameJob is the restore-time identity key: two jobs match when their name,
// schedule and command agree. IDs are panel-local and are deliberately
// ignored, which makes restore idempotent across panels and re-imports.
func SameJob(a, b Job) bool {
	return a.Name == b.Name && a.Schedule == b.Schedule && a.Command == b.Command
}

// FindMissing returns the wanted jobs that have no counterpart in existing.
func FindMissing(existing, wanted []Job) []Job {
	missing := make([]Job, 0, len(wanted))
	for _, w := range wanted {
		found := false
		for _, e := range existing {
			if SameJob(e, w) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, w)
		}
	}
	return missing
}

// ToggleLine flips the enabled state of the line tagged with id. Every other
// line is returned byte-identical so an unrelated edit never happens.
func ToggleLine(line, id string) string {
	job := ParseLine(line)
	if job == nil || job.ID != id {
		return line
	}
	if job.Enabled {
		return "#" + line
	}
	return strings.Replace(line, "#", "", 1)
}

// IsOurLine reports whether a crontab line is owned by this panel. It is used
// to decide whether a line may be dropped during delete.
func IsOurLine(line string) bool {
	return ParseLine(line) != nil
}
