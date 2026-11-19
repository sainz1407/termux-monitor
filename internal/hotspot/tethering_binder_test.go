package hotspot

import (
	"strings"
	"testing"
)

// The replies below were captured from an Android 10 device. The vendor ROM
// shifts the transaction codes of IConnectivityManager, so the tethering
// transactions do not sit at the AOSP indices.
const capturedProbeOutput = `@@21
Result: Parcel(ffff ffff 00000019 63616c6c 696e6750 6b6720 63616e6e 6f7420 626520 6e756c6c)
@@27
Result: Parcel(
  0x00000000: 00000000 00000002 00000005 006c0077 006e0061 00000030 00000005 006c0077 006e0061 00000031
)
@@30
Result: Parcel(00000000 00000001 0000000c 00390031 002e0032 00360031 002e0038 00320034 0032002e)
@@32
Result: Parcel(00000000 00000001 00000006 006c0077 006e0061 0064005c)
`

func TestParseTransactionReplies(t *testing.T) {
	replies := parseTransactionReplies(capturedProbeOutput)

	if len(replies) != 4 {
		t.Fatalf("expected 4 parsed replies, got %d", len(replies))
	}

	values, ok := decodeReplyStringArray(replies[27])
	if !ok {
		t.Fatalf("expected the tetherable interface list to be decodable")
	}
	if len(values) != 2 || values[0] != "wlan0" || values[1] != "wlan1" {
		t.Errorf("unexpected interface list: %v", values)
	}
}

func TestDeriveTetheringBinderCodes(t *testing.T) {
	replies := parseTransactionReplies(capturedProbeOutput)

	codes, err := deriveTetheringBinderCodes(replies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if codes.Start != 25 || codes.Stop != 26 {
		t.Errorf("expected start 25 and stop 26, got start %d and stop %d", codes.Start, codes.Stop)
	}
}

func TestDeriveTetheringBinderCodesWithoutInterfaceAnchor(t *testing.T) {
	// A device without a tetherable interface list only offers the regular
	// expression anchor of the wifi interfaces.
	replies := parseTransactionReplies(`@@32
Result: Parcel(00000000 00000001 00000006 006c0077 006e0061 0064005c)
`)

	codes, err := deriveTetheringBinderCodes(replies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if codes.Start != 25 || codes.Stop != 26 {
		t.Errorf("expected start 25 and stop 26, got start %d and stop %d", codes.Start, codes.Stop)
	}
}

func TestDeriveTetheringBinderCodesWithoutAnchor(t *testing.T) {
	replies := parseTransactionReplies(`@@18
Result: Parcel(00000000 00000000)
`)

	if _, err := deriveTetheringBinderCodes(replies); err == nil {
		t.Errorf("expected an error when no anchor is found")
	}
}

func TestReplyFailure(t *testing.T) {
	replies := parseTransactionReplies(capturedProbeOutput)

	if message := replyFailure(replies[27]); message != "" {
		t.Errorf("expected no failure for a successful reply, got %q", message)
	}

	failed := []uint32{replyExceptionMarker, 6}
	units := []uint16{'c', 'a', 'n', 'n', 'o', 't'}
	for i := 0; i < len(units); i += 2 {
		failed = append(failed, uint32(units[i])|uint32(units[i+1])<<16)
	}
	if message := replyFailure(failed); !strings.Contains(message, "cannot") {
		t.Errorf("expected the failure message to be decoded, got %q", message)
	}
}
