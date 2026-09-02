package store

import (
	"database/sql"

	"github.com/tedkulp/drops/internal/model"
)

// Optional columns are carried as SQL NULL, never as an empty string: the
// difference between "no assignee" and "the empty assignee" is real, and the
// mirror round trip has to preserve it.

func nullText(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullStamp(value *model.Timestamp) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func nullID(value *model.ID) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func textPtr(column sql.NullString) *string {
	if !column.Valid {
		return nil
	}
	value := column.String
	return &value
}

// stampPtr carries stored bytes through unchanged. Timestamps are validated
// where they enter the store, so a read never reformats what it found.
func stampPtr(column sql.NullString) *model.Timestamp {
	if !column.Valid {
		return nil
	}
	value := model.Timestamp(column.String)
	return &value
}

func idPtr(column sql.NullString) *model.ID {
	if !column.Valid {
		return nil
	}
	value := model.ID(column.String)
	return &value
}

func tombstoneValue(tombstone model.Tombstone) int {
	if tombstone {
		return 1
	}
	return 0
}

func tombstoneFrom(stored int) model.Tombstone {
	return model.Tombstone(stored != 0)
}
