// +build ignore

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const docURL = "https://developer.github.com/v3/activity/events/types"

var output string

func die(v interface{}) {
	fmt.Fprintln(os.Stderr, v)
	os.Exit(1)
}

func init() {
	flag.StringVar(&output, "o", "payloads.go", "")
	flag.Parse()
}

type rawEvent struct {
	name        string
	payloadJSON string
}

// Those keys that are assigned to null in example JSON payloads lack type
// information. Instead the value types are mapped here by hand.
var hardcodedTypes = map[string]string{
	"position":    "int",
	"line":        "int",
	"closed_at":   "time.Time",
	"merged_at":   "time.Time",
	"path":        "string",
	"homepage":    "string",
	"language":    "string",
	"mirror_url":  "string",
	"assignee":    "string",
	"milestone":   "string",
	"message":     "string",
	"merged_by":   "string",
	"base_ref":    "string",
	"name":        "string",
	"target_url":  "string",
	"description": "string",
}

func scrapPayload(s *goquery.Selection, n int) string {
	url, ok := s.Find("a").Attr("href")
	if !ok {
		die("unable to find URL for scrapping")
	}
	url = "https://developer.github.com" + url
	res, err := http.Get(url)
	if err != nil {
		die(err)
	}
	defer res.Body.Close()
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		die(err)
	}
	var payload string
	doc.Find(`div[class='content'] > pre[class='body-response'] > code[class^='language']`).Each(
		func(i int, s *goquery.Selection) {
			if i == n {
				payload = s.Text()
			}
		},
	)
	if payload == "" {
		die(fmt.Sprintf("unable to scrap %s (n=%d)", url, n))
	}
	return payload
}

func externalJSON(event *rawEvent, s *goquery.Selection) bool {
	switch event.name {
	case "DownloadEvent":
		event.payloadJSON = scrapPayload(s, 1)
		return true
	case "FollowEvent":
		event.payloadJSON = scrapPayload(s, 0)
		return true
	case "GistEvent":
		event.payloadJSON = fmt.Sprintf(`{"action":"create","gist":%s}`, scrapPayload(s, 1))
		return true
	case "ForkApplyEvent":
		event.payloadJSON = `{"head":"master","before":"e51831b1","after":"0c72c758c"}`
		return true
	default:
		return false
	}
}

func main() {
	res, err := http.Get(docURL)
	if err != nil {
		die(err)
	}
	defer res.Body.Close()
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		die(err)
	}
	var events []rawEvent
	var n int
	doc.Find(`div[class='content'] > h2[id$='event'],h3[id^='payload']+table,table+pre`).Each(
		func(i int, s *goquery.Selection) {
			switch {
			case n == len(events):
				events = append(events, rawEvent{name: s.Text()})
			case externalJSON(&events[n], s):
				n++
			default:
				s.Find(`pre > code`).Each(
					func(_ int, s *goquery.Selection) {
						if events[n].payloadJSON != "" {
							die(fmt.Sprintf("duplicate JSON payload for %q event (i=%d)", events[n].name, i))
						}
						events[n].payloadJSON = s.Text()
					})
				if events[n].payloadJSON != "" {
					n++
				}
			}
		})
	for i := range events {
		switch {
		case !strings.HasSuffix(events[i].name, "Event"):
			die(fmt.Sprintf("invalid event name: %q (i=%d)", events[i].name, i))
		case events[i].payloadJSON == "":
			die(fmt.Sprintf("empty payload for %q event (i=%d)", events[i].name, i))
		}
	}
}
