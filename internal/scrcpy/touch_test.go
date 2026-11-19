package scrcpy

import (
	"strings"
	"testing"
)

func collectTouchCommands(events []InputEvent) []string {
	var cmds []string
	state := &legacyTouchState{run: func(cmd string) { cmds = append(cmds, cmd) }}
	for _, evt := range events {
		state.handle(evt)
	}
	return cmds
}

func TestLegacyTouchTap(t *testing.T) {
	cmds := collectTouchCommands([]InputEvent{
		{Action: "down", X: 120, Y: 640},
		{Action: "up", X: 120, Y: 640},
	})

	if len(cmds) != 1 {
		t.Fatalf("expected a single command, got %v", cmds)
	}
	if cmds[0] != "input tap 120 640" {
		t.Fatalf("unexpected tap command: %q", cmds[0])
	}
}

func TestLegacyTouchDragBecomesSwipe(t *testing.T) {
	cmds := collectTouchCommands([]InputEvent{
		{Action: "down", X: 500, Y: 1500},
		{Action: "move", X: 500, Y: 1200},
		{Action: "move", X: 500, Y: 900},
		{Action: "up", X: 500, Y: 900},
	})

	if len(cmds) == 0 {
		t.Fatal("expected at least one swipe command")
	}
	if !strings.HasPrefix(cmds[0], "input swipe 500 1500 500 1200") {
		t.Fatalf("unexpected first drag command: %q", cmds[0])
	}
	for _, cmd := range cmds {
		if !strings.HasPrefix(cmd, "input swipe ") {
			t.Fatalf("unexpected drag command: %q", cmd)
		}
	}
	if strings.Contains(strings.Join(cmds, "|"), "input tap") {
		t.Fatalf("a drag must not be replayed as a tap: %v", cmds)
	}
}

func TestLegacyTouchIgnoresEventsWithoutPress(t *testing.T) {
	cmds := collectTouchCommands([]InputEvent{
		{Action: "move", X: 10, Y: 10},
		{Action: "up", X: 10, Y: 10},
	})

	if len(cmds) != 0 {
		t.Fatalf("expected no command for an unpressed gesture, got %v", cmds)
	}
}
