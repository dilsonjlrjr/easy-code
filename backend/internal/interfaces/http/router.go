package http

import (
	"strings"

	"github.com/valyala/fasthttp"

	"easycode/internal/domain/failure"
)

// HandlerFunc handles a request and returns a typed error instead of writing error bodies.
type HandlerFunc func(rc *fasthttp.RequestCtx) error

// Codes owned by the HTTP layer.
const (
	codeMethodNotAllowed     = "method-not-allowed"
	codeRequiredPrecondition = "required-precondition"
	codeFailedPrecondition   = "failed-precondition"
)

type route struct {
	method   string
	template string
	segments []string
	handler  HandlerFunc
}

// Router matches method and path templates with {name} segments.
type Router struct {
	routes    []route
	responder *Responder
}

// NewRouter builds an empty router that reports handler errors through responder.
func NewRouter(responder *Responder) *Router {
	return &Router{responder: responder}
}

// Handle registers a handler for a method and a path template.
func (r *Router) Handle(method, template string, handler HandlerFunc) {
	r.routes = append(r.routes, route{
		method:   method,
		template: template,
		segments: splitPath(template),
		handler:  handler,
	})
}

// Serve dispatches the request. It is the innermost fasthttp handler.
func (r *Router) Serve(rc *fasthttp.RequestCtx) {
	if err := r.dispatch(rc); err != nil {
		r.responder.Problem(rc, err)
	}
}

func (r *Router) dispatch(rc *fasthttp.RequestCtx) error {
	segments := splitPath(string(rc.Path()))
	pathMatched := false
	for _, candidate := range r.routes {
		params, ok := candidate.match(segments)
		if !ok {
			continue
		}
		pathMatched = true
		if candidate.method != string(rc.Method()) {
			continue
		}
		rc.SetUserValue(routeValue, candidate.template)
		rc.SetUserValue(paramsValue, params)
		return candidate.handler(rc)
	}
	if pathMatched {
		return failure.New(failure.KindInvalidInput, codeMethodNotAllowed, nil)
	}
	return failure.New(failure.KindNotFound, failure.CodeNotFound, nil)
}

func (candidate route) match(segments []string) (map[string]string, bool) {
	if len(segments) != len(candidate.segments) {
		return nil, false
	}
	params := map[string]string{}
	for i, expected := range candidate.segments {
		if name, isParam := paramName(expected); isParam {
			params[name] = segments[i]
			continue
		}
		if expected != segments[i] {
			return nil, false
		}
	}
	return params, true
}

func paramName(segment string) (string, bool) {
	if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
		return segment[1 : len(segment)-1], true
	}
	return "", false
}

func splitPath(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
}
