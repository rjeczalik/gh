gh [![GoDoc](https://godoc.org/github.com/rjeczalik/gh?status.svg)](https://godoc.org/github.com/rjeczalik/gh) [![Build Status](https://img.shields.io/travis/rjeczalik/gh/master.svg)](https://travis-ci.org/rjeczalik/gh "linux_amd64") [![Build status](https://img.shields.io/appveyor/ci/rjeczalik/gh.svg)](https://ci.appveyor.com/project/rjeczalik/gh "windows_amd64") [![Coverage Status](https://img.shields.io/coveralls/rjeczalik/gh/master.svg)](https://coveralls.io/r/rjeczalik/gh?branch=master)
======

Commands and packages for GitHub services.

*Installation*

```
~ $ go get -u github.com/rjeczalik/gh
```

### webhook [![GoDoc](https://godoc.org/github.com/rjeczalik/gh/webhook?status.svg)](https://godoc.org/github.com/rjeczalik/gh/webhook)

Package webhook implements middleware for GitHub Webhooks. User provides webhook service object that handles events delivered by GitHub. Webhook handler verifies payload signature delivered along with the event, unmarshals it to corresponding event struct and dispatches control to user service.

*Documentation*

[https://godoc.org/github.com/rjeczalik/gh/webhook](https://https://godoc.org/github.com/rjeczalik/gh/webhook)

*Examples*

Notify Slack's channel about recent push:
```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/rjeczalik/gh/webhook"
)

var (
	secret  = flag.String("secret", "", "GitHub webhook secret")
	token   = flag.String("token", "", "Slack API token")
	channel = flag.String("channel", "", "Slack channel name")
)

type slack struct{}

func (s slack) Push(e *webhook.PushEvent) {
	const format = "https://slack.com/api/chat.postMessage?token=%s&channel=%s&text=%s"
	text := url.QueryEscape(fmt.Sprintf("%s pushed to %s", e.Pusher.Email, e.Repository.Name))
	if _, err := http.Get(fmt.Sprintf(format, *token, *channel, text)); err != nil {
		log.Println(err)
	}
}

func main() {
	flag.Parse()
	log.Fatal(http.ListenAndServe(":8080", webhook.New(*secret, slack{})))
}
```
Notify HipChat's room about recent push:
```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/rjeczalik/gh/webhook"
)

var (
	secret = flag.String("secret", "", "GitHub webhook secret")
	token  = flag.String("token", "", "HipChat personal API token")
	room   = flag.String("room", "", "HipChat room ID")
)

type hipchat struct{}

func (h hipchat) Push(e *webhook.PushEvent) {
	url := fmt.Sprintf("https://api.hipchat.com/v2/room/%s/notification", *room)
	body := fmt.Sprintf(`{"message":"%s pushed to %s"}`, e.Pusher.Email, e.Repository.Name)
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		log.Println(err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+*token)
	if _, err := http.DefaultClient.Do(req); err != nil {
		log.Println(err)
	}
}

func main() {
	flag.Parse()
	log.Fatal(http.ListenAndServe(":8080", webhook.New(*secret, hipchat{})))
}
```

### cmd/webhook [![GoDoc](https://godoc.org/github.com/rjeczalik/gh/cmd/webhook?status.svg)](https://godoc.org/github.com/rjeczalik/gh/cmd/webhook)

Command webhook starts a web server which listens on GitHub's POST requests. The payload of each request is verified against its signature, unmarshalled into corresponding event struct and the applied to the template script provided by a user.

*Examples*

Notify Slack's channel about recent push:
```bash
~ $ cat >slack.tsc <<EOF
> {{with $e := .}}
>   {{if eq $e.Name "push"}}
>     {{with $text := (urlquery (printf "%s pushed to %s" $e.Payload.Pusher.Email $e.Payload.Repository.Name))}}
>     {{with $url := (printf "https://slack.com/api/chat.postMessage?token=%s&channel=%s&text=%s" (env "SLACK_TOKEN") (env "SLACK_CHANNEL") $text)}}
>       {{exec "curl" "-X" "GET" $url}}
>     {{end}}
>     {{end}}
>   {{end}}
> {{end}}
> EOF
```
```
~ $ webhook -secret secret123 slack.tsc
```
Notify HipChat's room about recent push:
```bash
~ $ cat >hipchat.tsc <<EOF 
> {{with $e := .}}
>   {{if eq $e.name "push"}}
>     {{with $auth := (printf "authorization: bearer %s" (env "hipchat_token"))}}
>     {{with $msg := (printf "{\"message_format\": \"text\", \"message\": \"%s pushed to %s\"}" $e.payload.pusher.email $e.payload.repository.name)}}
>     {{with $url := (printf "https://api.hipchat.com/v2/room/%s/notification" (env "hipchat_room"))}}
>       {{exec "curl" "-h" "content-type: application/json" "-h" $auth "-x" "post" "-d" $msg $url | log}}
>     {{end}}
>     {{end}}
>     {{end}}
>   {{end}}
> {{end}}
> EOF
```
```
~ $ webhook -secret secret123 hipchat.tsc
```
