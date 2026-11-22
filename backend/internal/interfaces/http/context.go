package http

import (
	"context"

	"github.com/valyala/fasthttp"
)

type userValueKey int

const (
	contextValue userValueKey = iota
	routeValue
	paramsValue
	userIDValue
)

// RequestContext returns the request-scoped context with request id and trace span.
func RequestContext(rc *fasthttp.RequestCtx) context.Context {
	if ctx, ok := rc.UserValue(contextValue).(context.Context); ok {
		return ctx
	}
	return context.Background()
}

func setRequestContext(ctx context.Context, rc *fasthttp.RequestCtx) {
	rc.SetUserValue(contextValue, ctx)
}

// RouteTemplate returns the matched route template, such as /api/v1/projects/{projectId}.
func RouteTemplate(rc *fasthttp.RequestCtx) string {
	if route, ok := rc.UserValue(routeValue).(string); ok {
		return route
	}
	return "unmatched"
}

// PathParam returns a named path parameter of the matched route.
func PathParam(rc *fasthttp.RequestCtx, name string) string {
	params, _ := rc.UserValue(paramsValue).(map[string]string)
	return params[name]
}

// SetUserID records the authenticated user id so the access log carries it.
func SetUserID(rc *fasthttp.RequestCtx, userID string) {
	rc.SetUserValue(userIDValue, userID)
}

func userID(rc *fasthttp.RequestCtx) string {
	id, _ := rc.UserValue(userIDValue).(string)
	return id
}
