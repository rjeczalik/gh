package main

import (
	"crypto/rand"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"text/template"

	"github.com/rjeczalik/gh/webhook"
)

const usage = `usage: webhook [-cert file -key file] [-addr address] -secret key script`

var (
	cert   = flag.String("cert", "", "Certificate file.")
	key    = flag.String("key", "", "Private key file.")
	addr   = flag.String("addr", "", "Network address to listen on. Default is :8080 for HTTP and :8443 for HTTPS.")
	secret = flag.String("secret", "", "GitHub secret value used for signing payloads.")
)

type Event struct {
	Name    string      // https://developer.github.com/webhooks/#events
	Payload interface{} // https://developer.github.com/v3/activity/events/types/
}

var scriptFuncs = template.FuncMap{
	"exec": func(cmd string, args ...string) (string, error) {
		out, err := exec.Command(cmd, args...).Output()
		return string(out), err
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

type handler struct {
	tmpl *template.Template
}

func newHandler(file string) (handler, error) {
	tmpl := template.New("webhook").Funcs(scriptFuncs)
	tmpl, err := tmpl.ParseFiles(flag.Arg(0))
	if err != nil {
		return handler{}, err
	}
	return handler{tmpl: tmpl}, nil
}

func (h handler) All(event string, payload interface{}) {
	if err := h.tmpl.Execute(ioutil.Discard, Event{Name: event, Payload: payload}); err != nil {
		log.Println("ERROR template error:", err)
		return
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
	handler, err := newHandler(flag.Arg(0))
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
	log.Printf("INFO Listening on %s . . .", listener.Addr())
	if err := http.Serve(listener, webhook.New(*secret, handler)); err != nil {
		die(err)
	}
}
