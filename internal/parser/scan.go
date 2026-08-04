package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const maxJSONLRecordSize = 64 * 1024 * 1024
const reverseSearchChunkSize = 64 * 1024

// fileSize returns the byte size of a file, or 0 if it cannot be stat'd.
func fileSize(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}

// OffsetOnRecordBoundary reports whether offset lands immediately after a
// newline in the file, i.e. on a JSONL record boundary. Incremental sync stores
// the previous file size as the resume offset; for a genuine append the byte
// before it is the '\n' terminating the last record. If an upstream tool
// rewrites the file in place (changing earlier bytes) rather than appending,
// this guards against seeking past mutated content and ingesting garbage: a
// false result tells callers to fall back to a full re-parse.
func OffsetOnRecordBoundary(path string, offset int64) bool {
	if offset <= 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 1)
	if _, err := f.ReadAt(buf, offset-1); err != nil {
		return false
	}
	return buf[0] == '\n'
}

// scanJSONL opens a JSONL file and invokes fn for each decoded object. Lines
// that fail to parse are skipped. Returning false from fn stops iteration.
func scanJSONL(path string, fn func(rec map[string]any) bool) {
	scanJSONLFrom(path, 0, fn)
}

// lastJSONLRecordContaining finds the newest record with an exact byte marker
// without decoding every record before it. Codex turns can contain multi-MB
// image payloads, so presence polling must be able to step over those records
// while looking for the small user_message / agent_message event rows.
func lastJSONLRecordContaining(path string, needle []byte) map[string]any {
	records := lastJSONLRecordsContaining(path, needle, 1)
	if len(records) == 0 {
		return nil
	}
	return records[0]
}

// lastJSONLRecordsContaining returns matching records newest first. It keeps
// the reverse scan in one pass so asking for a handful of recent activity
// updates does not repeatedly traverse a long transcript.
func lastJSONLRecordsContaining(path string, needle []byte, limit int) []map[string]any {
	if len(needle) == 0 || limit <= 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}

	var records []map[string]any
	end := info.Size()
	overlap := int64(len(needle) - 1)
	for end > 0 {
		start := end - reverseSearchChunkSize
		if start < 0 {
			start = 0
		}
		buf := make([]byte, end-start)
		n, readErr := f.ReadAt(buf, start)
		if readErr != nil && readErr != io.EOF {
			return nil
		}
		buf = buf[:n]
		if index := bytes.LastIndex(buf, needle); index >= 0 {
			position := start + int64(index)
			line, lineErr := jsonlLineContaining(f, position, info.Size())
			if lineErr == nil {
				var record map[string]any
				if json.Unmarshal(line, &record) == nil {
					records = append(records, record)
					if len(records) >= limit {
						return records
					}
				}
			}
			end = position
			continue
		}
		if start == 0 {
			break
		}
		end = start + overlap
	}
	return records
}

func jsonlLineContaining(f *os.File, position, fileSize int64) ([]byte, error) {
	lineStart := position
	for lineStart > 0 {
		start := lineStart - reverseSearchChunkSize
		if start < 0 {
			start = 0
		}
		buf := make([]byte, lineStart-start)
		n, err := f.ReadAt(buf, start)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if index := bytes.LastIndexByte(buf[:n], '\n'); index >= 0 {
			lineStart = start + int64(index) + 1
			break
		}
		lineStart = start
		if position-lineStart > maxJSONLRecordSize {
			return nil, fmt.Errorf("JSONL record exceeds %d bytes", maxJSONLRecordSize)
		}
	}

	lineEnd := position
	for lineEnd < fileSize {
		end := lineEnd + reverseSearchChunkSize
		if end > fileSize {
			end = fileSize
		}
		buf := make([]byte, end-lineEnd)
		n, err := f.ReadAt(buf, lineEnd)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if index := bytes.IndexByte(buf[:n], '\n'); index >= 0 {
			lineEnd += int64(index)
			break
		}
		lineEnd = end
		if lineEnd-lineStart > maxJSONLRecordSize {
			return nil, fmt.Errorf("JSONL record exceeds %d bytes", maxJSONLRecordSize)
		}
	}

	line := make([]byte, lineEnd-lineStart)
	_, err := f.ReadAt(line, lineStart)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return line, nil
}

// scanJSONLFrom is like scanJSONL but starts reading at byteOffset. The offset
// must fall on a record boundary (JSONL files are append-only, one object per
// line), which is how incremental sync resumes after previously read bytes.
func scanJSONLFrom(path string, byteOffset int64, fn func(rec map[string]any) bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if byteOffset > 0 {
		if _, err := f.Seek(byteOffset, io.SeekStart); err != nil {
			return
		}
	}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 256*1024), maxJSONLRecordSize)
	for s.Scan() {
		var r map[string]any
		if json.Unmarshal(s.Bytes(), &r) != nil {
			continue
		}
		if !fn(r) {
			return
		}
	}
	if err := s.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "(scan failed: %s: %v)\n", path, err)
	}
}
