package webhook

import (
	"reflect"
	"sort"
	"testing"
)

type Foo struct{}

func (Foo) All(string, interface{}) {}
func (Foo) Ping(*PingEvent)         {}

type Bar struct{}

func (Bar) Create(*CreateEvent) {}
func (Bar) Gist(*GistEvent)     {}
func (Bar) Push(*PushEvent)     {}

type Baz struct{}

func (Baz) All(string, interface{})   {}
func (Baz) Delete(*DeleteEvent)       {}
func (Baz) ForkApply(*ForkApplyEvent) {}
func (Baz) Gollum(*GollumEvent)       {}

func TestPayloadMethods(t *testing.T) {
	cases := [...]struct {
		rcvr   interface{}
		events []string
	}{
		// i=0
		{
			Foo{},
			[]string{"*", "ping"},
		},
		// i=1
		{
			Bar{},
			[]string{"create", "gist", "push"},
		},
		// i=2
		{
			Baz{},
			[]string{"*", "delete", "fork_apply", "gollum"},
		},
	}
	for i, cas := range cases {
		m := payloadMethods(reflect.TypeOf(cas.rcvr))
		events := make([]string, 0, len(m))
		for k := range m {
			events = append(events, k)
		}
		sort.StringSlice(events).Sort()
		if !reflect.DeepEqual(events, cas.events) {
			t.Errorf("want events=%v; got %v (i=%d)", cas.events, events, i)
		}
	}
}
