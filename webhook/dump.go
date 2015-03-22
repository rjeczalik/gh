package webhook

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func now() string {
	return time.Now().UTC().Format("2006-01-02 at 03.04.05.000")
}

func nonil(err ...error) error {
	for _, err := range err {
		if err != nil {
			return err
		}
	}
	return nil
}

func writefile(name string, p []byte, perm os.FileMode) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|os.O_SYNC, perm)
	if err != nil {
		return err
	}
	n, err := f.Write(p)
	if n < len(p) {
		err = nonil(err, io.ErrShortWrite)
	}
	return nonil(err, f.Sync(), f.Close())
}

type recorder struct {
	status int
	http.ResponseWriter
}

func record(w http.ResponseWriter) *recorder {
	return &recorder{ResponseWriter: w}
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

type dumper struct {
	dir     string
	log     *log.Logger
	handler http.Handler
}

// Dump is a helper handler, which wraps a webhook handler and dumps each
// request's body to a file when response was served successfully. It was
// added for *webhook.Handler in mind, but works on every generic http.Handler.
//
// If the destination directory is empty, Dump uses ioutil.TempDir instead.
// If the destination directory is a relative path, Dump uses filepath.Abs on it.
//
// If either of the above functions fails, Dump panics.
// If handler is a *webhook Handler and its ErrorLog field is non-nil, Dump uses
// it for logging.
func Dump(dir string, handler http.Handler) http.Handler {
	switch {
	case dir == "":
		name, err := ioutil.TempDir("", "webhook")
		if err != nil {
			panic(err)
		}
		dir = name
	default:
		name, err := filepath.Abs(dir)
		if err != nil {
			panic(err)
		}
		dir = name
		if err := os.MkdirAll(dir, 0755); err != nil {
			panic(err)
		}
	}
	d := &dumper{
		dir:     dir,
		handler: handler,
	}
	if handler, ok := handler.(*Handler); ok {
		d.log = handler.ErrorLog
	}
	return d
}

// ServeHTTP implements the http.Handler interface.
func (d dumper) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	buf := &bytes.Buffer{}
	rec := record(w)
	req.Body = ioutil.NopCloser(io.TeeReader(req.Body, buf))
	d.handler.ServeHTTP(rec, req)
	if rec.status == 200 {
		go d.dump(req.Header.Get("X-GitHub-Event"), buf)
	}
}

func (d dumper) dump(event string, buf *bytes.Buffer) {
	var name string
	if event != "" {
		name = filepath.Join(d.dir, fmt.Sprintf("%s-%s.json", event, now()))
	} else {
		name = filepath.Join(d.dir, now())
	}
	switch err := writefile(name, buf.Bytes(), 0644); err {
	case nil:
		d.logf("INFO %q: written file", name)
	default:
		d.logf("ERROR %q: error writing file: %v", name, err)
	}
}

func (d dumper) logf(format string, args ...interface{}) {
	if d.log != nil {
		d.log.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}
