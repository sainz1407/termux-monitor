package cron

import "testing"

func TestParseLineEnabled(t *testing.T) {
	line := "0 3 * * * /data/data/com.termux/files/usr/bin/backup.sh # tmon:ab12cd34:Nightly backup"
	job := ParseLine(line)
	if job == nil {
		t.Fatal("tagged line should parse")
	}
	if job.ID != "ab12cd34" {
		t.Errorf("ID = %q, want ab12cd34", job.ID)
	}
	if job.Name != "Nightly backup" {
		t.Errorf("Name = %q, want %q", job.Name, "Nightly backup")
	}
	if job.Schedule != "0 3 * * *" {
		t.Errorf("Schedule = %q", job.Schedule)
	}
	if job.Command != "/data/data/com.termux/files/usr/bin/backup.sh" {
		t.Errorf("Command = %q", job.Command)
	}
	if !job.Enabled {
		t.Error("unprefixed line should be enabled")
	}
}

func TestParseLineDisabled(t *testing.T) {
	line := "#0 3 * * * echo hi # tmon:deadbeef:Paused"
	job := ParseLine(line)
	if job == nil {
		t.Fatal("disabled tagged line should parse")
	}
	if job.Enabled {
		t.Error("hash-prefixed line should be disabled")
	}
	if job.Name != "Paused" {
		t.Errorf("Name = %q", job.Name)
	}
}

func TestParseLineLeadingWhitespaceBeforeHash(t *testing.T) {
	job := ParseLine("   #0 0 * * * true # tmon:0000000a:Indented")
	if job == nil {
		t.Fatal("indented disabled line should parse")
	}
	if job.Enabled {
		t.Error("should be disabled")
	}
}

func TestParseLineIgnoresForeignLines(t *testing.T) {
	// A foreign panel line, a plain line and a comment all parse to nil.
	cases := []string{
		"0 3 * * * /usr/bin/x # panel:1234:Other panel",
		"0 3 * * * /usr/bin/x",
		"# a plain comment",
		"",
		"0 3 * * * echo # tmon:",
		"0 3 * * * echo # tmon:NOTHEX:name",
	}
	for _, c := range cases {
		if job := ParseLine(c); job != nil {
			t.Errorf("ParseLine(%q) = %+v, want nil", c, job)
		}
	}
}

func TestParseLineRejectsTooFewFields(t *testing.T) {
	// Only four schedule fields before the tag: not a crontab entry.
	if job := ParseLine("0 3 * * # tmon:ab12cd34:x"); job != nil {
		t.Errorf("expected nil, got %+v", job)
	}
}

func TestParseLineMultiWordCommand(t *testing.T) {
	job := ParseLine("*/5 * * * * sh -c \"echo hello world\" # tmon:aabbccdd:Ping")
	if job == nil {
		t.Fatal("expected a job")
	}
	if job.Command != `sh -c "echo hello world"` {
		t.Errorf("Command = %q", job.Command)
	}
}

func TestJobLineRoundTrip(t *testing.T) {
	line := JobLine("ab12cd34", "Nightly backup", "0 3 * * *", "/bin/backup.sh")
	job := ParseLine(line)
	if job == nil {
		t.Fatal("rendered line should parse")
	}
	if job.ID != "ab12cd34" || job.Name != "Nightly backup" ||
		job.Schedule != "0 3 * * *" || job.Command != "/bin/backup.sh" {
		t.Errorf("round trip mismatch: %+v", job)
	}
}

func TestJobLineSanitisesName(t *testing.T) {
	line := JobLine("ab12cd34", "evil\nname # tmon:beef:injected", "* * * * *", "true")
	if got := len(splitOnNewline(line)); got != 1 {
		t.Fatalf("rendered line contains %d lines, want 1: %q", got, line)
	}
	job := ParseLine(line)
	if job == nil {
		t.Fatal("sanitised line should still parse")
	}
	if job.ID != "ab12cd34" {
		t.Errorf("ID = %q, want ab12cd34 (injection changed the id)", job.ID)
	}
}

// splitOnNewline counts the physical lines in s without importing strings.
func splitOnNewline(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func TestToggleLine(t *testing.T) {
	line := "0 3 * * * true # tmon:ab12cd34:X"
	off := ToggleLine(line, "ab12cd34")
	if off != "#"+line {
		t.Errorf("disable = %q", off)
	}
	back := ToggleLine(off, "ab12cd34")
	if back != line {
		t.Errorf("re-enable = %q, want %q", back, line)
	}
}

func TestToggleLineLeavesOtherLines(t *testing.T) {
	lines := []string{
		"0 3 * * * true # tmon:aaaaaaaa:A",
		"0 4 * * * false # tmon:bbbbbbbb:B",
		"0 5 * * * /bin/foreign",
	}
	// Toggling A must not touch B or the foreign line.
	got := ToggleLine(lines[1], "aaaaaaaa")
	if got != lines[1] {
		t.Errorf("unrelated line changed: %q", got)
	}
	got = ToggleLine(lines[2], "aaaaaaaa")
	if got != lines[2] {
		t.Errorf("foreign line changed: %q", got)
	}
}

func TestSameJobIgnoresID(t *testing.T) {
	a := Job{ID: "11111111", Name: "n", Schedule: "0 1 * * *", Command: "true"}
	b := Job{ID: "22222222", Name: "n", Schedule: "0 1 * * *", Command: "true"}
	if !SameJob(a, b) {
		t.Error("same content should match regardless of id")
	}
	b.Command = "false"
	if SameJob(a, b) {
		t.Error("differing command should not match")
	}
}

func TestFindMissing(t *testing.T) {
	existing := []Job{{Name: "a", Schedule: "* * * * *", Command: "true"}}
	wanted := []Job{
		{Name: "a", Schedule: "* * * * *", Command: "true"},
		{Name: "b", Schedule: "0 0 * * *", Command: "false"},
	}
	missing := FindMissing(existing, wanted)
	if len(missing) != 1 || missing[0].Name != "b" {
		t.Errorf("FindMissing = %+v, want only b", missing)
	}
}

func TestIsOurLine(t *testing.T) {
	if !IsOurLine("0 3 * * * true # tmon:ab12cd34:X") {
		t.Error("tagged line should be recognised")
	}
	if IsOurLine("0 3 * * * true") {
		t.Error("plain line should not be ours")
	}
}

func TestIsHex(t *testing.T) {
	if !isHex("ab12cd34") {
		t.Error("lowercase hex should pass")
	}
	if isHex("") || isHex("AB12") || isHex("xyz") {
		t.Error("empty, uppercase and non-hex should fail")
	}
}
