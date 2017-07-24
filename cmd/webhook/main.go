// Command webhook starts a web server which listens on GitHub's POST requests.
// The payload of each request is verified against its signature, unmarshalled
// into corresponding event struct and the applied to the template script provided
// by a user.
//
// Usage
//
//   webhook [-cert file -key file] [-addr address] [-log file] -secret key script
//
// The struct being passed to the template script is:
//
//   type Event struct {
//   	Name    string
//   	Payload interface{}
//   	Args    map[string]string
//   }
//
// The Name field denotes underlying type for the Payload. Full mapping between
// possible Name values and Payload types is listed in the documentation of
// the webhook package. The Args field contains all command line flags passed
// to template script.
//
// Template scripts use template syntax of text/template package. Each template
// script has registered extra control functions:
//
//   env
//   	An alias for os.Getenv.
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
// Template scripts input
//
// Template scripts support currently two of ways accepting input:
//
//   - via {{env "VARIABLE"}} function
//   - and via command lines arguments
//
// Positional arguments that follow double-dash argument are turned into map[string]string
// value, which is then passed as Args field of an Event.
//
// Example
//
// The command line arguments passed after -- for the following command line
//
//   $ webhook -secret secret123 examples/slack.tsc -- -token token123 -channel CH123
//
// are passed to the script as
//
//   ...
//   Args: map[string]string{
//   	"Token":   "token123",
//   	"Channel": "CH123",
//   },
//   ...
//
// The -cert and -key flags are used to provide paths for the certificate and private
// key files. When specified, webhook serves HTTPS connections by default on 0.0.0.0:8443.
//
// The -addr flag can be used to specify a network address for the webhook to listen on.
//
// The -secret flag sets the secret value to verify the signature of GitHub's payloads.
// The value is required and cannot be empty.
//
// The -log flag redirects output to the given file.
//
// The -dump flag makes webhook dump each received JSON payload into specified
// directory. The file is named after <event>-<delivery>.json, where:
//
//   - <event> is a value of X-GitHub-Event header
//   - <delivery> is a value of X-GitHub-Delivery header
//
// The script argument is a path to the template script file which is used as a handler
// for incoming events.
package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/rjeczalik/gh/cmd/internal/tsc"
	"github.com/rjeczalik/gh/webhook"
)

const usage = `usage: webhook [-cert file -key file] [-addr address] [-log file] -secret key script

Starts a web server which listens on GitHub's POST requests. The payload of each
request is verified against its signature, unmarshalled into corresponding event
struct and the applied to the template script provided by a user.

The struct being passed to the template script is:

	type Event struct {
		Name    string
		Payload interface{}
		Args    map[string]string
	}

The Name field denotes underlying type for the Payload. Full mapping between
possible Name values and Payload types is listed in the documentation of
the webhook package. The Args field contains all command line flags passed
to template script.

Template scripts use template syntax of text/template package. Each template
script has registered extra control functions:

	env
		An alias for os.Getenv.
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

Template scripts input

Template scripts support currently two of ways accepting input:

	- via {{env "VARIABLE"}} function
	- and via command lines arguments

Positional arguments that follow double-dash argument are turned into map[string]string
value, which is then passed as Args field of an Event.

Example

The command line arguments passed after -- for the following command line

	$ webhook -secret secret123 examples/slack.tsc -- -token token123 -channel CH123

are passed to the script as

	...
	Args: map[string]string{
		"Token":   "token123",
		"Channel": "CH123",
	},
	...

The -cert and -key flags are used to provide paths for certificate and private
key files. When specified, webhook serves HTTPS connection by default on 0.0.0.0:8443.

The -addr flag can be used to specify a network address for the webhook to listen on.

The -secret flag sets the secret value to verify the signature of GitHub's payloads.
The value is required and cannot be empty.

The -log flag redirects output to the given file.

The -dump flag makes webhook dump each received JSON payload into specified
directory. The file is named after <event>-<delivery>.json, where:

	- <event> is a value of X-GitHub-Event header
	- <delivery> is a value of X-GitHub-Delivery header

The script argument is a path to the template script file which is used as a handler
for incoming events.`

var config struct {
	Cert       string   `json:"cert"`
	Key        string   `json:"key"`
	Addr       string   `json:"addr"`
	Secret     string   `json:"secret"`
	Debug      bool     `json:"debug"`
	Dump       string   `json:"dump"`
	Log        string   `json:"log"`
	Script     string   `json:"script"`
	ScriptArgs []string `json:"scriptArgs"`
}

var configFile = flag.String("config", "", "Configuration file to use.")

func init() {
	flag.StringVar(&config.Cert, "cert", "", "Certificate file.")
	flag.StringVar(&config.Key, "key", "", "Private key file.")
	flag.StringVar(&config.Addr, "addr", "", "Network address to listen on. Default is :8080 for HTTP and :8443 for HTTPS.")
	flag.StringVar(&config.Secret, "secret", "", "GitHub secret value used for signing payloads.")
	flag.BoolVar(&config.Debug, "debug", false, "Dumps verified payloads into testdata directory.")
	flag.StringVar(&config.Dump, "dump", "", "Dumps verified payloads into given directory.")
	flag.StringVar(&config.Log, "log", "", "Redirects output to the given file.")
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
	if flag.NArg() > 0 {
		config.Script = flag.Arg(0)
	}
	if *configFile != "" {
		p, err := ioutil.ReadFile(*configFile)
		if os.IsNotExist(err) {
			if p, err = json.Marshal(&config); err != nil {
				die(err)
			}
			if err = os.MkdirAll(filepath.Dir(*configFile), 0700); err != nil {
				die(err)
			}
			if err = ioutil.WriteFile(*configFile, p, 0600); err != nil {
				die(err)
			}
		}
		if err = json.Unmarshal(p, &config); err != nil {
			die(err)
		}
	}
	if config.Script == "" {
		die("missing script file")
	}
	if (config.Cert == "") != (config.Key == "") {
		die("both -cert and -key flags must be provided")
	}
	if config.Debug && config.Dump == "" {
		config.Dump = "testdata"
	}
	var arg string
	config.ScriptArgs = os.Args
	for len(config.ScriptArgs) != 0 {
		arg, config.ScriptArgs = config.ScriptArgs[0], config.ScriptArgs[1:]
		if arg == "--" {
			break
		}
	}
	if config.Log != "" {
		f, err := os.OpenFile(config.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			die(err)
		}
		log.SetOutput(f)
		defer f.Close()
	}
	sc, err := tsc.New(config.Script, config.ScriptArgs)
	if err != nil {
		die(err)
	}
	var listener net.Listener
	if config.Cert != "" {
		crt, err := tls.LoadX509KeyPair(config.Cert, config.Key)
		if err != nil {
			die(err)
		}
		cfg := &tls.Config{
			Certificates: []tls.Certificate{crt},
		}
		l, err := tls.Listen("tcp", nonil(config.Addr, "0.0.0.0:8443"), cfg)
		if err != nil {
			die(err)
		}
		listener = l
	} else {
		l, err := net.Listen("tcp", nonil(config.Addr, "0.0.0.0:8080"))
		if err != nil {
			die(err)
		}
		listener = l
	}
	var handler http.Handler = webhook.New(config.Secret, sc)
	if config.Dump != "" {
		handler = webhook.Dump(config.Dump, handler)
	}
	log.Printf("INFO Listening on %s . . .", listener.Addr())
	if err := http.Serve(listener, handler); err != nil {
		die(err)
	}
}
