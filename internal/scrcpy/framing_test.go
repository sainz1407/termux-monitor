package scrcpy

import "testing"

// TestIsAVCCSample guards the framing detection: scrcpy-server output buffers
// are already length-prefixed, so they must be recognized instead of being run
// through the Annex-B conversion (which dropped every sample and left clients
// with a blank canvas).
func TestIsAVCCSample(t *testing.T) {
	avcc := []byte{0x00, 0x00, 0x00, 0x05, 0x65, 0x88, 0x84, 0x00, 0x21}
	if !isAVCCSample(avcc) {
		t.Fatalf("expected length-prefixed payload to be detected as AVCC")
	}

	annexB := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84}
	if isAVCCSample(annexB) {
		t.Fatalf("expected Annex-B payload not to be detected as AVCC")
	}

	if isAVCCSample(nil) {
		t.Fatalf("expected empty payload not to be detected as AVCC")
	}

	// Truncated length prefix must not be accepted.
	truncated := []byte{0x00, 0x00, 0x00, 0x40, 0x65}
	if isAVCCSample(truncated) {
		t.Fatalf("expected truncated payload not to be detected as AVCC")
	}
}

// TestSampleToAVCC verifies both framing paths produce the same normalized
// output shape expected by WebCodecs.
func TestSampleToAVCC(t *testing.T) {
	sample := []byte{0x00, 0x00, 0x00, 0x05, 0x65, 0x88, 0x84, 0x00, 0x21}
	out := sampleToAVCC(sample)
	if out == nil || len(out) != len(sample) {
		t.Fatalf("expected AVCC passthrough of %d bytes, got %v", len(sample), out)
	}
	for i := range sample {
		if out[i] != sample[i] {
			t.Fatalf("passthrough mismatch at %d", i)
		}
	}

	annexB := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84}
	converted := sampleToAVCC(annexB)
	want := []byte{0x00, 0x00, 0x00, 0x03, 0x65, 0x88, 0x84}
	if len(converted) != len(want) {
		t.Fatalf("expected %d bytes, got %v", len(want), converted)
	}
	for i := range want {
		if converted[i] != want[i] {
			t.Fatalf("conversion mismatch at %d: want %v, got %v", i, want, converted)
		}
	}
}
