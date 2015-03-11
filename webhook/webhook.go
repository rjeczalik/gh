// Package webhook implements server handling for GitHub Webhooks POST requests.
package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"reflect"
)

//go:generate go run generate_payloads.go -t -o payloads.go
//go:generate gofmt -w -s payloads.go

const maxPayloadLen = 1024 * 1024 * 1024 // 1MiB

var errMethod = errors.New("invalid HTTP method")
var errHeaders = errors.New("invalid HTTP headers")
var errSig = errors.New("invalid signature header")
var errPayload = errors.New("unsupported payload type")

type Handler struct {
	// ErrorLog specifies an optional logger for errors serving requests.
	// If nil, logging goes to os.Stderr via the log package's standard logger.
	ErrorLog *log.Logger

	secret []byte                    // value for X-Hub-Signature
	rcvr   reflect.Value             // receiver of methods for the service
	method map[string]reflect.Method // event handling methods
}

func New(secret string, rcvr interface{}) *Handler {
	if secret == "" {
		panic("webhook: called New with empty secret")
	}
	h := &Handler{
		secret: []byte(secret),
		rcvr:   reflect.ValueOf(rcvr),
		method: make(map[string]reflect.Method),
	}
	return h
}

// ServeHTTP implements the http.Handler interface.
func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	event := req.Header.Get("X-GitHub-Event")
	sig := []byte(req.Header.Get("X-Hub-Signature"))
	switch {
	case req.Method != "POST":
		h.fatal(w, req, http.StatusMethodNotAllowed, errMethod)
		return
	case event == "" || len(sig) == 0:
		h.fatal(w, req, http.StatusBadRequest, errHeaders)
		return
	case req.ContentLength <= 0 || req.ContentLength > maxPayloadLen:
		h.fatal(w, req, http.StatusBadRequest, errHeaders)
		return
	}
	body := make([]byte, int(req.ContentLength))
	_, err := req.Body.Read(body)
	if err != nil && err != io.EOF {
		h.fatal(w, req, http.StatusInternalServerError, err)
		return
	}
	mac := hmac.New(sha256.New, h.secret)
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), sig) {
		h.fatal(w, req, http.StatusUnauthorized, errSig)
		return
	}
	typ, ok := payloadTypes[event]
	if !ok {
		h.fatal(w, req, http.StatusBadRequest, errPayload)
		return
	}
	v := reflect.New(typ)
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(v.Interface()); err != nil {
		h.fatal(w, req, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusOK)
	go h.call(req.RemoteAddr, event, v.Interface())
}

func (h *Handler) call(remote, event string, payload interface{}) {
	if method, ok := h.method[event]; ok {
		method.Func.Call([]reflect.Value{h.rcvr, reflect.ValueOf(payload)})
		h.logf("%s: Status=200 X-GitHub-Event=%q Type=%T", remote, event, payload)
		return
	}
	if all, ok := h.method["*"]; ok {
		all.Func.Call([]reflect.Value{h.rcvr, reflect.ValueOf(event), reflect.ValueOf(payload)})
		h.logf("%s: Status=200 X-GitHub-Event=%q Type=%T", remote, event, payload)
		return
	}
	if event == "ping" {
		h.logf("%s: Status=200 X-GitHub-Event=ping Events=%v", remote, payload.(*PingEvent).Hook.Events)
	}
}

func (h *Handler) fatal(w http.ResponseWriter, req *http.Request, code int, err error) {
	h.logf("%s: Status=%d X-GitHub-Event=%q Content-Length=%d: %v", req.RemoteAddr,
		req.Header.Get("X-GitHub-Event"), req.ContentLength, err)
	http.Error(w, http.StatusText(code), code)
}

func (h *Handler) logf(format string, args ...interface{}) {
	if h.ErrorLog != nil {
		h.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}
