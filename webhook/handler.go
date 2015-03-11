package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
)

const maxPayloadLen = 1024 * 1024 * 1024 // 1MiB

var errMethod = errors.New("invalid HTTP method")
var errHeaders = errors.New("invalid HTTP headers")
var errSig = errors.New("invalid signature header")
var errPayload = errors.New("unsupported payload type")

var empty = reflect.TypeOf(func(interface{}) {}).In(0)

// payloadMethods loosly bases around suitableMethods from $GOROOT/src/net/rpc/server.go.
func payloadMethods(typ reflect.Type) map[string]reflect.Method {
	methods := make(map[string]reflect.Method)
LoopMethods:
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		mtype := method.Type
		mname := method.Name
		if method.PkgPath != "" {
			continue LoopMethods
		}
		switch mtype.NumIn() {
		case 2:
			eventType := mtype.In(1)
			if eventType.Kind() != reflect.Ptr {
				log.Println("method", mname, "takes wrong type of event:", eventType)
				continue LoopMethods
			}
			event, ok := payloads.Name(eventType.Elem())
			if !ok {
				log.Println("method", mname, "takes wrong type of event:", eventType)
				continue LoopMethods
			}
			if _, ok = methods[event]; ok {
				panic(fmt.Sprintf("there is more than one method handling %v event", eventType))
			}
			methods[event] = method
		case 3:
			if mtype.In(1).Kind() != reflect.String || mtype.In(2) != empty {
				log.Println("wildcard method", mname, "takes wrong types of arguments")
				continue LoopMethods
			}
			if _, ok := methods["*"]; ok {
				panic("there is more than one method handling all events")
			}
			methods["*"] = method
		default:
			log.Println("method", mname, "takes wrong number of arguments:", mtype.NumIn())
			continue LoopMethods
		}
	}
	return methods
}

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
	return &Handler{
		secret: []byte(secret),
		rcvr:   reflect.ValueOf(rcvr),
		method: payloadMethods(reflect.TypeOf(rcvr)),
	}
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
	typ, ok := payloads.Type(event)
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
		code, req.Header.Get("X-GitHub-Event"), req.ContentLength, err)
	http.Error(w, http.StatusText(code), code)
}

func (h *Handler) logf(format string, args ...interface{}) {
	if h.ErrorLog != nil {
		h.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}
