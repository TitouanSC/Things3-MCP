package backend

import (
	"errors"
	"testing"

	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

func TestPaginateWalksAllItems(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	var got []int
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > len(items) {
			t.Fatal("pagination does not terminate")
		}
		p, err := Paginate(items, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p.Items...)
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if len(got) != len(items) {
		t.Fatalf("got %v, want %v", got, items)
	}
	for i := range items {
		if got[i] != items[i] {
			t.Fatalf("got %v, want %v", got, items)
		}
	}
}

func TestPaginatePastEnd(t *testing.T) {
	p, err := Paginate([]int{1}, EncodeOffsetCursor(10), 5)
	if err != nil || len(p.Items) != 0 || p.NextCursor != "" {
		t.Fatalf("Paginate past end = %+v, %v", p, err)
	}
}

func TestDecodeOffsetCursorRejectsGarbage(t *testing.T) {
	for _, c := range []string{"!!", "bm9wZQ", EncodeOffsetCursor(-1)} {
		if _, err := DecodeOffsetCursor(c); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("DecodeOffsetCursor(%q) = %v, want ErrValidation", c, err)
		}
	}
}
