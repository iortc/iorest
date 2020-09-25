package iorest

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
)

func StrCaseEqual(a, b string) bool {
	return strings.EqualFold(a, b)
}

type Error struct {
	Code   int    `json:"error"`
	Reason string `json:"reason"`
}

func (e Error) Error() string {
	return e.Reason
}

func Errorf(code int, format string, v ...interface{}) Error {
	return Error{Code: code, Reason: fmt.Sprintf(format, v...)}
}

type Context struct {
	request *http.Request
	paths   []string
	resType string
	resCode int
}

func (c *Context) Warningf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

func (c *Context) Errorf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

func (c *Context) ClientAddress() (string, error) {
	host, _, err := net.SplitHostPort(c.request.RemoteAddr)
	return host, err
}

func (c *Context) Method() string {
	return c.request.Method
}

func (c *Context) URI() string {
	return c.request.URL.Path
}

func (c *Context) Path(i int) string {
	if i >= len(c.paths) {
		return ""
	}
	return c.paths[i]
}

func (c *Context) FormValue(name, preset string) string {
	str := c.request.Form.Get(name)
	if str == "" {
		str = preset
	}
	return str
}

func (c *Context) IsTLS() bool {
	return c.request.TLS != nil
}

func (c *Context) Host() string {
	return c.request.Host
}

func (c *Context) ParseJson(data interface{}) error {
	dec := json.NewDecoder(c.request.Body)
	return dec.Decode(data)
}

func (c *Context) SetResourceType(t string) {
	c.resType = t
}

func (c *Context) SetErrorResponseCode(code int) {
	c.resCode = code
}

type Handler func(*Context) (interface{}, error)

type Server struct {
	Prefix     string
	registered bool
	handlers   map[string]Handler
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == "OPTIONS" {
		return
	}
	suffix := r.URL.Path[len(s.Prefix):]
	splits := strings.Split(suffix, "/")
	resource := splits[0]
	handler := s.handlers[resource]
	if handler == nil {
		http.Error(w, fmt.Sprintf("No such resource '%s'", resource), http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var err error
	ctx := &Context{request: r, paths: splits, resType: "application/json", resCode: -1}
	res, err := handler(ctx)
	io.Copy(ioutil.Discard, r.Body)
	if err != nil {
		switch err.(type) {
		case Error:
			ctx.Warningf("%s %s restful error: %d %s", r.Method, r.URL.Path, err.(Error).Code, err.Error())
			res = err
		default:
			ctx.Warningf("%s %s error: %s", r.Method, r.URL.Path, err.Error())
			code := http.StatusInternalServerError
			if ctx.resCode != -1 {
				code = ctx.resCode
			}
			http.Error(w, err.Error(), code)
			return
		}
	}
	w.Header().Set("Content-Type", ctx.resType)
	if ctx.resType == "application/json" {
		if res == nil {
			res = make(map[string]interface{})
		}
		enc := json.NewEncoder(w)
		// enc.SetIndent("", "    ")
		if err = enc.Encode(res); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if isByteArray(res) == false {
			ctx.Errorf("Resource is not byte array.")
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		bytes := res.([]byte)
		off := 0
		for off < len(bytes) {
			n, err := w.Write(res.([]byte))
			if err != nil {
				ctx.Errorf("Failed to write byttes: %s", err.Error())
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			off = off + n
		}
	}
}

var typeOfBytes = reflect.TypeOf([]byte(nil))

func isByteArray(a interface{}) bool {
	v := reflect.ValueOf(a)
	return v.Kind() == reflect.Slice && v.Type() == typeOfBytes
}

func (s *Server) HandleFunc(resource string, handler Handler) {
	if !s.registered {
		http.HandleFunc(s.Prefix, s.serveHTTP)
		s.registered = true
	}
	if s.handlers == nil {
		s.handlers = make(map[string]Handler)
	}
	s.handlers[resource] = handler
}
