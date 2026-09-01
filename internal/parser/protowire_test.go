package parser

import (
	"encoding/binary"
	"math"
	"testing"
)

// protoVarintBytes and the builders below are the encoding counterpart of
// protowire.go. They exist only for tests: nothing in ATM writes protobuf, so an
// encoder in the package proper would be code with no caller.
func protoVarintBytes(value uint64) []byte {
	buf := make([]byte, binary.MaxVarintLen64)
	return buf[:binary.PutUvarint(buf, value)]
}

func protoTag(number, wire int) []byte {
	return protoVarintBytes(uint64(number)<<3 | uint64(wire))
}

// protoEncodeVarint appends field number = value.
func protoEncodeVarint(number int, value int64) []byte {
	return append(protoTag(number, protoWireVarint), protoVarintBytes(uint64(value))...)
}

// protoEncodeBytes appends field number = payload, length-prefixed. Used for both
// strings and nested messages, which share wire type 2.
func protoEncodeBytes(number int, payload []byte) []byte {
	out := protoTag(number, protoWireBytes)
	out = append(out, protoVarintBytes(uint64(len(payload)))...)
	return append(out, payload...)
}

func protoEncodeString(number int, value string) []byte {
	return protoEncodeBytes(number, []byte(value))
}

func TestProtoReadersRoundTrip(t *testing.T) {
	inner := protoEncodeVarint(1, 42)
	inner = append(inner, protoEncodeString(2, "hello")...)
	message := protoEncodeBytes(7, inner)
	message = append(message, protoEncodeVarint(3, 99)...)

	if got, ok := protoInt64(message, 7, 1); !ok || got != 42 {
		t.Fatalf("nested varint = %d, %v", got, ok)
	}
	if got, ok := protoString(message, 7, 2); !ok || got != "hello" {
		t.Fatalf("nested string = %q, %v", got, ok)
	}
	if got, ok := protoInt64(message, 3); !ok || got != 99 {
		t.Fatalf("top-level varint = %d, %v", got, ok)
	}
	if _, ok := protoInt64(message, 7, 5); ok {
		t.Fatal("absent field reported present")
	}
	if _, ok := protoSub(message, 3); ok {
		t.Fatal("varint field returned as a submessage")
	}
}

// A field repeated in the wire resolves to the last occurrence for a singular
// read, and protoRepeated returns all of them in order.
func TestProtoRepeatedAndLastWins(t *testing.T) {
	message := protoEncodeString(1, "first")
	message = append(message, protoEncodeString(1, "second")...)

	if got, ok := protoString(message, 1); !ok || got != "second" {
		t.Fatalf("singular read = %q, %v; want the last occurrence", got, ok)
	}
	all := protoRepeated(message, 1)
	if len(all) != 2 || string(all[0]) != "first" || string(all[1]) != "second" {
		t.Fatalf("protoRepeated = %q", all)
	}
}

func TestProtoDurationMS(t *testing.T) {
	duration := protoEncodeVarint(1, 15)
	duration = append(duration, protoEncodeVarint(2, 107273000)...)
	message := protoEncodeBytes(9, duration)

	got, ok := protoDurationMS(message, 9)
	if !ok || got != 15107 {
		t.Fatalf("protoDurationMS = %d, %v; want 15107", got, ok)
	}
	// Seconds only is a valid Duration and must not be reported as absent.
	secondsOnly := protoEncodeBytes(9, protoEncodeVarint(1, 3))
	if got, ok := protoDurationMS(secondsOnly, 9); !ok || got != 3000 {
		t.Fatalf("seconds-only duration = %d, %v", got, ok)
	}
	if _, ok := protoDurationMS(protoEncodeBytes(9, nil), 9); ok {
		t.Fatal("empty duration submessage reported present")
	}
}

// These blobs are written by another process while ATM reads them, so truncated
// and nonsensical input is routine. None of it may panic, and none of it may be
// mistaken for a value.
func TestProtoReadersRejectMalformed(t *testing.T) {
	valid := protoEncodeBytes(1, protoEncodeVarint(2, 7))
	cases := []struct {
		name string
		buf  []byte
	}{
		{"truncated mid-message", valid[:len(valid)-1]},
		{"length beyond buffer", append(protoTag(1, protoWireBytes), protoVarintBytes(200)...)},
		{"field number zero", append(protoTag(0, protoWireVarint), 1)},
		{"unsupported wire type", append(protoTag(1, 3), 1)},
		{"truncated fixed64", append(protoTag(1, protoWireI64), 1, 2, 3)},
		{"truncated fixed32", append(protoTag(1, protoWireI32), 1)},
		{"varint overflow", append(protoTag(1, protoWireVarint),
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)},
		{"random bytes", []byte{0xde, 0xad, 0xbe, 0xef}},
		{"empty", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Not asserting a particular verdict — a garbage buffer may happen to
			// be a decodable message. The contract is that nothing panics and no
			// read goes out of bounds.
			_, _ = protoSub(tc.buf, 1)
			_, _ = protoInt64(tc.buf, 1)
			_, _ = protoInt64(tc.buf, 1, 2)
			_, _ = protoString(tc.buf, 1)
			_, _ = protoDurationMS(tc.buf, 1)
			_ = protoRepeated(tc.buf, 1)
			_ = eachProtoField(tc.buf, func(protoField) bool { return true })
		})
	}
}

// A count that cannot fit in int64 is reported absent rather than wrapped
// negative: a negative token count would corrupt every total it reached.
func TestProtoInt64RejectsOversizedValues(t *testing.T) {
	message := append(protoTag(1, protoWireVarint), protoVarintBytes(math.MaxUint64)...)
	if got, ok := protoInt64(message, 1); ok {
		t.Fatalf("oversized varint returned %d as present", got)
	}
}

// An empty path has no field to read and must not be answered from the buffer
// itself.
func TestProtoReadersRejectEmptyPath(t *testing.T) {
	message := protoEncodeVarint(1, 5)
	if _, ok := protoInt64(message); ok {
		t.Fatal("protoInt64 with no path reported present")
	}
	if _, ok := protoString(message); ok {
		t.Fatal("protoString with no path reported present")
	}
}
