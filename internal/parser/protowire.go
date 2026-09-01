package parser

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// A minimal protobuf wire-format reader, for upstreams that store their
// transcripts as serialized messages whose .proto is not published.
//
// This is deliberately not generated code and adds no dependency. Antigravity
// writes its per-request accounting as an opaque blob in a SQLite column; ATM
// needs six fields out of it, identified by field number from observation. There
// is no schema to generate from, so google.golang.org/protobuf would buy nothing
// but a reflection and registry stack — and it would still be dynamic access by
// field number at the end of it.
//
// Everything here treats its input as hostile. These blobs belong to another
// process that is writing them while ATM reads, so a truncated message, a
// nonsense length prefix or a field number of zero are all normal outcomes, and
// none of them may panic or read out of bounds. Malformed input is reported as
// "not present" so a parser degrades to missing values rather than failing a
// whole sync.

// Protobuf wire types. Groups (3 and 4) are not supported: they were deprecated
// before any of these formats existed, and skipping a group requires tracking
// nesting, which would be code with no caller.
const (
	protoWireVarint = 0
	protoWireI64    = 1
	protoWireBytes  = 2
	protoWireI32    = 5
)

var errProtoMalformed = errors.New("malformed protobuf")

// protoField is one decoded field occurrence. Bytes holds the payload for
// wire type 2 and is nil otherwise; Num holds the numeric value for the fixed
// and varint wire types and is 0 otherwise.
type protoField struct {
	Number int
	Wire   int
	Bytes  []byte
	Num    uint64
}

// eachProtoField walks a message's top-level fields in encoding order, calling
// visit for each. visit returning false stops the walk early and is not an
// error. A malformed message reports errProtoMalformed after the fields decoded
// before the damage, so a caller that only needs early fields still gets them.
func eachProtoField(buf []byte, visit func(f protoField) bool) error {
	for offset := 0; offset < len(buf); {
		key, read := binary.Uvarint(buf[offset:])
		if read <= 0 {
			return errProtoMalformed
		}
		offset += read
		number, wire := int(key>>3), int(key&7)
		// Field 0 is not assignable, so seeing it means this is not a message —
		// most often a byte string that happened to survive the length check.
		if number == 0 {
			return errProtoMalformed
		}
		field := protoField{Number: number, Wire: wire}
		switch wire {
		case protoWireVarint:
			value, read := binary.Uvarint(buf[offset:])
			if read <= 0 {
				return errProtoMalformed
			}
			offset += read
			field.Num = value
		case protoWireI64:
			if len(buf)-offset < 8 {
				return errProtoMalformed
			}
			field.Num = binary.LittleEndian.Uint64(buf[offset:])
			offset += 8
		case protoWireBytes:
			length, read := binary.Uvarint(buf[offset:])
			if read <= 0 {
				return errProtoMalformed
			}
			offset += read
			// The length is attacker-controlled and may exceed both the remaining
			// bytes and int range on 32-bit builds; compare in uint64 before any
			// conversion so the check cannot itself overflow.
			if length > uint64(len(buf)-offset) {
				return errProtoMalformed
			}
			field.Bytes = buf[offset : offset+int(length)]
			offset += int(length)
		case protoWireI32:
			if len(buf)-offset < 4 {
				return errProtoMalformed
			}
			field.Num = uint64(binary.LittleEndian.Uint32(buf[offset:]))
			offset += 4
		default:
			// Groups and any future wire type: the rest of the message cannot be
			// skipped safely without knowing its size.
			return fmt.Errorf("%w: unsupported wire type %d", errProtoMalformed, wire)
		}
		if !visit(field) {
			return nil
		}
	}
	return nil
}

// protoBytes returns the last occurrence of a length-delimited field. Last, not
// first, because that is what proto3 merge semantics say a repeated-in-the-wire
// singular field resolves to; protoRepeated is for fields that are genuinely
// repeated.
func protoBytes(buf []byte, number int) ([]byte, bool) {
	var found []byte
	var ok bool
	_ = eachProtoField(buf, func(f protoField) bool {
		if f.Number == number && f.Wire == protoWireBytes {
			found, ok = f.Bytes, true
		}
		return true
	})
	return found, ok
}

// protoRepeated returns every length-delimited occurrence of a field, in
// encoding order.
func protoRepeated(buf []byte, number int) [][]byte {
	var out [][]byte
	_ = eachProtoField(buf, func(f protoField) bool {
		if f.Number == number && f.Wire == protoWireBytes {
			out = append(out, f.Bytes)
		}
		return true
	})
	return out
}

// protoSub walks a path of field numbers into nested messages and returns the
// bytes of the message at the end of it. A missing or non-message field anywhere
// along the path reports false, so callers can read a deep field in one call
// without a guard per level.
func protoSub(buf []byte, path ...int) ([]byte, bool) {
	current := buf
	for _, number := range path {
		next, ok := protoBytes(current, number)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

// protoVarint reads a varint (or fixed32/fixed64) field, following path into
// nested messages first. The last element of path is the field being read.
func protoVarint(buf []byte, path ...int) (uint64, bool) {
	if len(path) == 0 {
		return 0, false
	}
	parent, ok := protoSub(buf, path[:len(path)-1]...)
	if !ok {
		return 0, false
	}
	number := path[len(path)-1]
	var value uint64
	var found bool
	_ = eachProtoField(parent, func(f protoField) bool {
		if f.Number == number && f.Wire != protoWireBytes {
			value, found = f.Num, true
		}
		return true
	})
	return value, found
}

// protoInt64 is protoVarint for the token counts and timestamps read here, all
// of which are non-negative in practice. A value that does not fit in int64 is
// reported as absent rather than wrapped negative: a negative token count would
// corrupt every total it reaches.
func protoInt64(buf []byte, path ...int) (int64, bool) {
	value, ok := protoVarint(buf, path...)
	if !ok || value > 1<<62 {
		return 0, false
	}
	return int64(value), true
}

// protoString reads a length-delimited field as text, following path into nested
// messages. The bytes are not validated as UTF-8: callers that index the result
// treat it as an opaque identifier, and the ones that store it hand it to SQLite,
// which accepts either.
func protoString(buf []byte, path ...int) (string, bool) {
	if len(path) == 0 {
		return "", false
	}
	parent, ok := protoSub(buf, path[:len(path)-1]...)
	if !ok {
		return "", false
	}
	value, ok := protoBytes(parent, path[len(path)-1])
	if !ok {
		return "", false
	}
	return string(value), true
}

// protoDurationMS reads a google.protobuf.Duration or Timestamp submessage
// (field 1 seconds, field 2 nanos) as milliseconds. Both types share that
// layout, which is why one reader serves the request duration and nothing else
// needs to know which of the two it was handed.
func protoDurationMS(buf []byte, path ...int) (int64, bool) {
	message, ok := protoSub(buf, path...)
	if !ok {
		return 0, false
	}
	seconds, hasSeconds := protoInt64(message, 1)
	nanos, hasNanos := protoInt64(message, 2)
	if !hasSeconds && !hasNanos {
		return 0, false
	}
	return seconds*1000 + nanos/1e6, true
}
