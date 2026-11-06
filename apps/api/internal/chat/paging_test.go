package chat

import (
	"strconv"
	"testing"
)

func TestPaginateSetsCursorOnlyWhenMoreExist(t *testing.T) {
	key := func(n int) string { return strconv.Itoa(n) }
	double := func(n int) int { return n * 2 }

	page := paginate([]int{1, 2, 3}, 2, key, double)
	if len(page.Items) != 2 || page.Items[1] != 4 || page.NextCursor != "2" {
		t.Fatalf("with extra row: %+v", page)
	}
	page = paginate([]int{1, 2}, 2, key, double)
	if len(page.Items) != 2 || page.NextCursor != "" {
		t.Fatalf("exactly limit: %+v", page)
	}
	if page = paginate([]int(nil), 2, key, double); page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("empty page must serialize as []: %+v", page)
	}
}

func TestPageOptionsDefaultsAndValidation(t *testing.T) {
	if got, err := pageOptions(PageOptions{}); err != nil || got.Limit != defaultPageSize {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []PageOptions{{Limit: -1}, {Limit: maxPageSize + 1}, {Cursor: "nope"}} {
		if _, err := pageOptions(bad); err != ErrInvalidInput {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestSetContinuation(t *testing.T) {
	page := HistoryPage{HasMore: true, Messages: []Message{{Seq: 4}, {Seq: 5}}}
	page.SetContinuation(true)
	if page.NextAfterSeq == nil || *page.NextAfterSeq != 5 || page.NextBeforeSeq != nil {
		t.Fatalf("forward: %+v", page)
	}
	page.SetContinuation(false)
	if page.NextBeforeSeq == nil || *page.NextBeforeSeq != 4 || page.NextAfterSeq != nil {
		t.Fatalf("backward: %+v", page)
	}
	page.HasMore = false
	page.SetContinuation(true)
	if page.NextAfterSeq != nil || page.NextBeforeSeq != nil {
		t.Fatalf("no more: %+v", page)
	}
}
