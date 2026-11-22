package http

import "github.com/valyala/fasthttp"

// register adds the product routes of openapi.yaml implemented so far.
func (a *api) register(router *Router) {
	auth := a.auth.require
	router.Handle(fasthttp.MethodGet, APIPrefix+"/session/login", a.startWebLogin)
	router.Handle(fasthttp.MethodGet, APIPrefix+"/session/oidc/callback", a.completeWebLogin)
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
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/configuration", auth(a.getProjectConfiguration))
		router.Handle(fasthttp.MethodPatch, APIPrefix+"/projects/{projectId}/configuration", auth(a.modifyProjectConfiguration))
	}
	if a.deps.Members != nil {
		router.Handle(fasthttp.MethodGet, APIPrefix+"/users", auth(a.searchUsers))
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/members", auth(a.listMembers))
		router.Handle(fasthttp.MethodPut, APIPrefix+"/projects/{projectId}/members/{userId}", auth(a.setMemberRoles))
		router.Handle(fasthttp.MethodDelete, APIPrefix+"/projects/{projectId}/members/{userId}", auth(a.removeMember))
	}
	if a.deps.Workflow != nil {
		router.Handle(fasthttp.MethodPost, APIPrefix+"/projects/{projectId}/start", auth(a.startWorkflow))
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/commands/{commandId}", auth(a.getCommand))
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/workflow", auth(a.getWorkflow))
		if a.deps.Waves != nil {
			router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/stages/{stage}", auth(a.getStage))
		}
	}
	if a.deps.Artifacts != nil {
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/artifacts", auth(a.listArtifacts))
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/artifacts/{artifactId}", auth(a.getArtifact))
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/artifacts/{artifactId}/versions", auth(a.listArtifactVersions))
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/artifacts/{artifactId}/versions/{version}", auth(a.getArtifactVersion))
	}
	if a.deps.Waves != nil {
		p := APIPrefix + "/projects/{projectId}/waves"
		router.Handle(fasthttp.MethodGet, p, auth(a.listWaves))
		router.Handle(fasthttp.MethodGet, p+"/{waveId}", auth(a.getWave))
		router.Handle(fasthttp.MethodGet, p+"/{waveId}/questions", auth(a.listWaveQuestions))
		router.Handle(fasthttp.MethodPost, p+"/{waveId}/questions/{questionId}/answer", auth(a.answerQuestion))
		router.Handle(fasthttp.MethodGet, p+"/{waveId}/checkpoints", auth(a.listCheckpoints))
		router.Handle(fasthttp.MethodGet, p+"/{waveId}/calls", auth(a.listWaveCalls))
		router.Handle(fasthttp.MethodGet, p+"/{waveId}/sandbox-executions", auth(a.listSandboxExecutions))
		router.Handle(fasthttp.MethodGet, p+"/{waveId}/sandbox-executions/{executionId}/output", auth(a.downloadExecutionOutput))
	}
	if a.deps.Gates != nil {
		p := APIPrefix + "/projects/{projectId}/gates/{gateId}/rounds"
		router.Handle(fasthttp.MethodGet, p, auth(a.listGateRounds))
		router.Handle(fasthttp.MethodGet, p+"/{round}", auth(a.getGateRound))
		router.Handle(fasthttp.MethodPost, p+"/{round}/approval", auth(a.approveGate))
		router.Handle(fasthttp.MethodPost, p+"/{round}/rejection", auth(a.rejectGate))
	}
	if a.deps.Repository != nil {
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/repository", auth(a.getRepository))
		// Git smart HTTP, read-only (outside openapi.yaml): HTTP Basic with the session token.
		router.Handle(fasthttp.MethodGet, "/git/{repository}/info/refs", a.gitReferences)
		router.Handle(fasthttp.MethodPost, "/git/{repository}/git-upload-pack", a.gitUploadPack)
	}
	if a.deps.Pending != nil {
		router.Handle(fasthttp.MethodGet, APIPrefix+"/pending-items", auth(a.listPendingItems))
	}
	if a.deps.Events != nil && a.deps.Notices != nil {
		router.Handle(fasthttp.MethodGet, APIPrefix+"/me/events", auth(a.watchNotices))
	}
	if a.deps.Events != nil {
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/events", auth(a.watchProjectEvents))
		router.Handle(fasthttp.MethodGet, APIPrefix+"/projects/{projectId}/events/history", auth(a.listProjectEvents))
	}
}
