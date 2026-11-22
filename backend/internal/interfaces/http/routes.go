package http

import "github.com/valyala/fasthttp"

// register adds the product routes of openapi.yaml implemented so far.
func (a *api) register(router *Router) {
	auth := a.auth.require
	if a.deps.Sessions.DevEnabled() {
		router.Handle(fasthttp.MethodPost, APIPrefix+"/dev/session", a.devSession)
	}
	router.Handle(fasthttp.MethodGet, APIPrefix+"/me", auth(a.currentUser))
	router.Handle(fasthttp.MethodPost, APIPrefix+"/session/logout", auth(a.endSession))

	if a.deps.Providers != nil {
		router.Handle(fasthttp.MethodGet, APIPrefix+"/providers", auth(a.listProviders))
		router.Handle(fasthttp.MethodPut, APIPrefix+"/providers/{provider}/key", auth(a.saveProviderKey))
		router.Handle(fasthttp.MethodDelete, APIPrefix+"/providers/{provider}/key", auth(a.removeProviderKey))
	}
}
