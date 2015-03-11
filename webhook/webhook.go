// Package webhook implements server handling for GitHub Webhooks POST requests.
package webhook

import (
	"bytes"
	"reflect"
	"strconv"
	"time"
)

//go:generate go run generate_payloads.go -t -o payloads.go
//go:generate go test -run TestGenerateMockHelper -- -generate mock_test.go
//go:generate gofmt -w -s payloads.go mock_test.go

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

type payloadsMap map[string]reflect.Type

func (p payloadsMap) Type(name string) (reflect.Type, bool) {
	typ, ok := p[name]
	return typ, ok
}

func (p payloadsMap) Name(typ reflect.Type) (string, bool) {
	for pname, ptyp := range p {
		if ptyp == typ {
			return pname, true
		}
	}
	return "", false
}
