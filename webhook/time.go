package webhook

import (
	"bytes"
	"strconv"
	"time"
)

var null = []byte("null")

// Time embeds time.Time. The wrapper allows for unmarshalling time from JSON
// null value or unix timestamp.
type Time struct {
	time.Time
}

// MarshalJSON implements the json.Marshaler interface. The time is a quoted
// string in RFC 3339 format or "null" if it's a zero value.
func (t Time) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return null, nil
	}
	return t.Time.MarshalJSON()
}

// UnmarshalJSON implements the json.Unmarshaler interface. The time is expected
// to be a quoted string in RFC 3339 format, a unix timestamp or a "null" string.
func (t *Time) UnmarshalJSON(p []byte) (err error) {
	if bytes.Compare(p, null) == 0 {
		t.Time = time.Time{}
		return nil
	}
	if err = t.Time.UnmarshalJSON(p); err == nil {
		return nil
	}
	n, e := strconv.ParseInt(string(bytes.Trim(p, `"`)), 10, 64)
	if e != nil {
		return err
	}
	t.Time = time.Unix(n, 0)
	return nil
}
