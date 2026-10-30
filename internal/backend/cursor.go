package backend

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

const offsetPrefix = "o:"

// EncodeOffsetCursor returns an opaque cursor for offset-based pagination.
func EncodeOffsetCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(offsetPrefix + strconv.Itoa(offset)))
}

// DecodeOffsetCursor decodes a cursor made by EncodeOffsetCursor. An empty
// cursor means the first page.
func DecodeOffsetCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !strings.HasPrefix(string(raw), offsetPrefix) {
		return 0, domain.Invalid("cursor", "malformed cursor")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(string(raw), offsetPrefix))
	if err != nil || n < 0 {
		return 0, domain.Invalid("cursor", "malformed cursor")
	}
	return n, nil
}

// Paginate slices items for a cursor and limit, computing the next cursor.
func Paginate[T any](items []T, cursor string, limit int) (Page[T], error) {
	offset, err := DecodeOffsetCursor(cursor)
	if err != nil {
		return Page[T]{}, err
	}
	if offset >= len(items) {
		return Page[T]{Items: []T{}}, nil
	}
	end := min(offset+limit, len(items))
	p := Page[T]{Items: items[offset:end]}
	if end < len(items) {
		p.NextCursor = EncodeOffsetCursor(end)
	}
	return p, nil
}
