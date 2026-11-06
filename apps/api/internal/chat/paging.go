package chat

const (
	defaultPageSize = 20
	maxPageSize     = 50
)

func pageOptions(options PageOptions) (PageOptions, error) {
	if options.Limit < 0 || options.Limit > maxPageSize {
		return PageOptions{}, ErrInvalidInput
	}
	if options.Limit == 0 {
		options.Limit = defaultPageSize
	}
	if options.Cursor != "" {
		var err error
		if options.Cursor, err = parseID(options.Cursor); err != nil {
			return PageOptions{}, err
		}
	}
	return options, nil
}

// trimPage takes records fetched with limit+1 rows. When the extra row is
// present it is dropped and the last kept record's key becomes the cursor.
func trimPage[R any](records []R, limit int, key func(R) string) ([]R, string) {
	if len(records) <= limit {
		return records, ""
	}
	return records[:limit], key(records[limit-1])
}

// paginate is trimPage followed by converting every record.
func paginate[R, T any](records []R, limit int, key func(R) string, convert func(R) T) Page[T] {
	records, next := trimPage(records, limit, key)
	page := Page[T]{Items: make([]T, 0, len(records)), NextCursor: next}
	for _, record := range records {
		page.Items = append(page.Items, convert(record))
	}
	return page
}
