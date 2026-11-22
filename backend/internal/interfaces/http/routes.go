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
	if a.deps.Pricing != nil {
		router.Handle(fasthttp.MethodGet, APIPrefix+"/models", auth(a.listModels))
		router.Handle(fasthttp.MethodPut, APIPrefix+"/providers/{provider}/models/{model}", auth(a.recordModel))
		router.Handle(fasthttp.MethodGet, APIPrefix+"/providers/{provider}/models/{model}/prices", auth(a.listModelPrices))
		router.Handle(fasthttp.MethodPost, APIPrefix+"/providers/{provider}/models/{model}/prices", auth(a.recordModelPrice))
		router.Handle(fasthttp.MethodPatch, APIPrefix+"/providers/{provider}/models/{model}/prices/{effectivePeriodId}", auth(a.correctModelPrice))
	}
	if a.deps.Projects != nil {
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects", auth(a.listProjects))
		router.Handle(fasthttp.MethodPost, APIPrefix+"/projects", auth(a.createProject))
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}", auth(a.getProject))
	}
}
