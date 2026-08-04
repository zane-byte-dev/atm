package cmd

import "fmt"

func paginate[T any](values []T, offset, limit int) ([]T, error) {
	if offset < 0 {
		return nil, fmt.Errorf("offset must not be negative")
	}
	if limit < 0 {
		return nil, fmt.Errorf("limit must not be negative")
	}
	if offset >= len(values) {
		return []T{}, nil
	}
	end := len(values)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return values[offset:end], nil
}
