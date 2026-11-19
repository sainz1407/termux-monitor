package store

import (
	"reflect"
	"testing"
)

func TestTermuxGroups(t *testing.T) {
	// The real Termux process on the reference device runs with
	// groups=3003 9997 20171 50171 for uid 10171 (user 0 app 171).
	got := termuxGroups(10171)
	want := []int{3003, 9997, 20171, 50171}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("termuxGroups(10171) = %v, want %v", got, want)
	}

	// A different user index must shift the derived groups accordingly.
	// uid 1030171 = user 10, app 20171: userOffset 10*100000 folds into the
	// per-user groups (AID_USER_OFFSET), giving 1040171 and 1070171.
	got = termuxGroups(1030171)
	want = []int{3003, 9997, 1040171, 1070171}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("termuxGroups(1030171) = %v, want %v", got, want)
	}
}

func TestTermuxGroupsAlwaysIncludesInet(t *testing.T) {
	// AID_INET (3003) is the group Android's paranoid-networking hook checks;
	// losing it is the exact bug this package must never reintroduce.
	for _, uid := range []int{10000, 10171, 10999, 1030171} {
		found := false
		for _, g := range termuxGroups(uid) {
			if g == aidInet {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("termuxGroups(%d) = %v, missing AID_INET %d", uid, termuxGroups(uid), aidInet)
		}
	}
}

func TestSuArgsWithGroups(t *testing.T) {
	// Bypass the cached probe so the asserted shape is deterministic.
	suGroupsMu.Lock()
	prev := suGroupsState
	suGroupsState = 1
	suGroupsMu.Unlock()
	defer func() {
		suGroupsMu.Lock()
		suGroupsState = prev
		suGroupsMu.Unlock()
	}()

	got := suArgs("su", 10171, "echo hi")
	want := []string{"10171", "-G", "3003", "-G", "9997", "-G", "20171", "-G", "50171", "-c", "echo hi"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suArgs with groups = %v, want %v", got, want)
	}
}

func TestSuArgsWithoutGroups(t *testing.T) {
	suGroupsMu.Lock()
	prev := suGroupsState
	suGroupsState = 0
	suGroupsMu.Unlock()
	defer func() {
		suGroupsMu.Lock()
		suGroupsState = prev
		suGroupsMu.Unlock()
	}()

	got := suArgs("su", 10171, "echo hi")
	want := []string{"10171", "-c", "echo hi"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suArgs without groups = %v, want %v", got, want)
	}
}
