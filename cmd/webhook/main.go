// Command webhook starts a web server which listens on GitHub's POST requests.
// The payload of each request is verified against its signature, unmarshalled
// into corresponding event struct and the applied to the template script provided
// by a user.
//
// Usage
//
//   webhook [-cert file -key file] [-addr address] -secret key script
//
//
// The struct being passed to the template script is:
//
//   type Event struct {
//   	Name    string
//   	Payload interface{}
//   }
//
// The Name field denotes underlying type for the Payload. Full mapping between
// possible Name values and Payload types is listed in the documentation of
// the webhook package.
//
// Template scripts use template syntax of text/template package. Each template
// script has registered extra control functions:
//
//   log
//   	An alias for log.Println. Used only for side-effect, returns empty string.
//   logf
//   	An alias for log.Printf. Used only for side-effect, returns empty string.
//   exec
//   	An alias for exec.Command. Returned value is the process' output read
//   	from its os.Stdout.
//
// Example
//
// In order to log an e-mail of each person that pushed to your repository, create
// a template script with the following content:
//
//   $ cat >push.tsc <<EOF
//   > {{if .Name | eq "push"}}
//   >   {{logf "%s pushed to %s" .Payload.Pusher.Email .Payload.Repository.Name}}
//   > {{endif}}
//   > EOF
//
// And start the webhook:
//
//   $ webhook -secret secret123 push.tsc
//   2015/03/13 21:32:15 INFO Listening on [::]:8080 . . .
//
// Webhook listens on 0.0.0.0:8080 by default.
//
// The -cert and -key flags are used to provide paths for certificate and private
// key files. When specified, webhook serves HTTPS connection by default on 0.0.0.0:8443.
//
// The -addr flag can be used to specify a network address for the webhook to listen on.
//
// The -secret flag sets the secret value to verify the signature of GitHub's payloads.
// The value is required and cannot be empty.
//
// The script argument is a path to the template script file which is used as a handler
// for incoming events.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
	"time"

	"github.com/rjeczalik/gh/webhook"
)

const usage = `usage: webhook [-cert file -key file] [-addr address] -secret key script

Starts a web server which listens on GitHub's POST requests. The payload of each
request is verified against its signature, unmarshalled into corresponding event
struct and the applied to the template script provided by a user.

The struct being passed to the template script is:

	type Event struct {
		Name    string
		Payload interface{}
	}

The Name field denotes underlying type for the Payload. Full mapping between
possible Name values and Payload types is listed in the documentation of
the webhook package.

Template scripts use template syntax of text/template package. Each template
script has registered extra control functions:

	log
		An alias for log.Println. Used only for side-effect, returns empty string.
	logf
		An alias for log.Printf. Used only for side-effect, returns empty string.
	exec
		An alias for exec.Command. Returned value is the process' output read
		from its os.Stdout.

Example

In order to log an e-mail of each person that pushed to your repository, create
a template script with the following content:

	$ cat >push.tsc <EOF
	> {{if .Name eq "push"}}
	>   {{logf "%s pushed to %s" .Payload.Pusher.Email .Payload.Repository.Name}}
	> {{endif}}
	> EOF

And start the webhook:

	$ webhook -secret secret123 push.tsc
	2015/03/13 21:32:15 INFO Listening on [::]:8080 . . .

Webhook listens on 0.0.0.0:8080 by default.

The -cert and -key flags are used to provide paths for certificate and private
key files. When specified, webhook serves HTTPS connection by default on 0.0.0.0:8443.

The -addr flag can be used to specify a network address for the webhook to listen on.

The -secret flag sets the secret value to verify the signature of GitHub's payloads.
The value is required and cannot be empty.

The script argument is a path to the template script file which is used as a handler
for incoming events.`

var (
	cert   = flag.String("cert", "", "Certificate file.")
	key    = flag.String("key", "", "Private key file.")
	addr   = flag.String("addr", "", "Network address to listen on. Default is :8080 for HTTP and :8443 for HTTPS.")
	secret = flag.String("secret", "", "GitHub secret value used for signing payloads.")
	debug  = flag.Bool("debug", false, "Dumps verified payloads into testdata directory.")
)

type Event struct {
	Name    string      // https://developer.github.com/webhooks/#events
	Payload interface{} // https://developer.github.com/v3/activity/events/types/
}

var scriptFuncs = template.FuncMap{
	"exec": func(cmd string, args ...string) (string, error) {
		out, err := exec.Command(cmd, args...).Output()
		return string(bytes.TrimSpace(out)), err
	},
	"log": func(v ...interface{}) string {
		log.Println(v...)
		return ""
	},
	"logf": func(format string, v ...interface{}) string {
		if len(v) == 0 {
			log.Printf("%s", format)
		} else {
			log.Printf(format, v...)
		}
		return ""
	},
}

type templater struct {
	tmpl *template.Template
}

func newTemplater(file string) (templater, error) {
	tmpl := template.New(filepath.Base(file)).Funcs(scriptFuncs)
	tmpl, err := tmpl.ParseFiles(flag.Arg(0))
	if err != nil {
		return templater{}, err
	}
	return templater{tmpl: tmpl}, nil
}

func (h templater) All(event string, payload interface{}) {
	if err := h.tmpl.Execute(ioutil.Discard, Event{Name: event, Payload: payload}); err != nil {
		log.Println("ERROR template error:", err)
		return
	}
}

type dumper struct {
	http.Handler
}

func (d dumper) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var buf bytes.Buffer
	req.Body = ioutil.NopCloser(io.TeeReader(req.Body, &buf))
	d.Handler.ServeHTTP(w, req)
	go dump(req.Header.Get("X-GitHub-Event"), buf.Bytes())
}

func now() string {
	return time.Now().UTC().Format("2006-01-02 at 03.04.05,00")
}

func dump(event string, p []byte) {
	switch {
	case event == "":
		log.Println("[DEBUG] ERROR empty event name")
		return
	case len(p) == 0:
		log.Println("[DEBUG] ERROR empty payload")
		return
	}
	if err := os.MkdirAll("testdata", 0755); err != nil {
		log.Println("[DEBUG] ERROR creating testdata:", err)
		return
	}
	name := filepath.Join("testdata", fmt.Sprintf("%s-%s.json", event, now()))
	if err := ioutil.WriteFile(name, p, 0644); err != nil {
		log.Printf("[DEBUG] ERROR creating %s: %v", name, err)
	}
}

func nonil(s ...string) string {
	for _, s := range s {
		if s != "" {
			return s
		}
	}
	return ""
}

func die(v interface{}) {
	fmt.Fprintln(os.Stderr, v)
	os.Exit(1)
}

func main() {
	if len(os.Args) == 1 {
		die(usage)
	}
	flag.CommandLine.Usage = func() {
		fmt.Fprintln(os.Stderr, usage)
	}
	flag.Parse()
	if flag.NArg() != 1 || flag.Arg(0) == "" {
		die("invalid number of arguments")
	}
	if (*cert == "") != (*key == "") {
		die("both -cert and -key flags must be provided")
	}
	tmpl, err := newTemplater(flag.Arg(0))
	if err != nil {
		die(err)
	}
	var listener net.Listener
	if *cert != "" {
		crt, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			die(err)
		}
		cfg := &tls.Config{
			Certificates: []tls.Certificate{crt},
			Rand:         rand.Reader,
			// Don't offer SSL3.
			MinVersion: tls.VersionTLS10,
			// Don't offer RC4 ciphers.
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
				tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
				tls.TLS_RSA_WITH_AES_128_CBC_SHA,
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			},
		}
		l, err := tls.Listen("tcp", nonil(*addr, "0.0.0.0:8443"), cfg)
		if err != nil {
			die(err)
		}
		listener = l
	} else {
		l, err := net.Listen("tcp", nonil(*addr, "0.0.0.0:8080"))
		if err != nil {
			die(err)
		}
		listener = l
	}
	var handler http.Handler = webhook.New(*secret, tmpl)
	if *debug {
		handler = dumper{Handler: handler}
	}
	log.Printf("INFO Listening on %s . . .", listener.Addr())
	if err := http.Serve(listener, handler); err != nil {
		die(err)
	}
}
