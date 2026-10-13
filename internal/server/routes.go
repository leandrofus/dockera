package server

import (
	"net/http"
)

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth", s.authInfo)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.auth(s.me))
	mux.HandleFunc("GET /api/audit", s.admin(s.auditList))

	// agent endpoints
	mux.HandleFunc("GET /api/agent/binary", s.serveAgentBinary)
	mux.HandleFunc("GET /api/agent/install.sh", s.serveAgentInstallScript)
	mux.HandleFunc("GET /api/agent/tunnel", s.handleAgentTunnel)

	// global settings
	mux.HandleFunc("GET /api/endpoints", s.auth(s.listEndpoints))
	mux.HandleFunc("POST /api/endpoints", s.admin(s.saveEndpoint))
	mux.HandleFunc("DELETE /api/endpoints/{name}", s.admin(s.deleteEndpoint))
	mux.HandleFunc("GET /api/endpoints/{name}/agent-install", s.admin(s.agentInstall))
	mux.HandleFunc("GET /api/registries", s.admin(s.listRegistries))
	mux.HandleFunc("POST /api/registries", s.admin(s.saveRegistry))
	mux.HandleFunc("DELETE /api/registries/{name}", s.admin(s.deleteRegistry))
	mux.HandleFunc("GET /api/settings", s.admin(s.getSettings))
	mux.HandleFunc("GET /api/settings/access", s.admin(s.accessInfo))
	mux.HandleFunc("POST /api/settings/access/test", s.admin(s.accessTest))
	mux.HandleFunc("PUT /api/settings", s.admin(s.putSettings))
	mux.HandleFunc("POST /api/settings/rotate-token", s.admin(s.rotateToken))
	mux.HandleFunc("GET /api/templates", s.auth(s.listTemplates))
	mux.HandleFunc("GET /api/templates/compose", s.auth(s.templateCompose))
	mux.HandleFunc("GET /api/tunnels", s.auth(s.listTunnels))
	mux.HandleFunc("DELETE /api/tunnels/{id}", s.admin(s.closeTunnel))

	e := func(pattern string, h http.HandlerFunc) {
		method, path, _ := cut(pattern)
		mux.HandleFunc(method+" /api/e/{ep}"+path, s.auth(h))
	}
	e("GET /info", s.info)
	e("GET /df", s.diskUsage)
	e("GET /events", s.events)
	e("POST /prune", s.systemPrune)

	e("GET /containers", s.listContainers)
	e("POST /containers", s.createContainer)
	e("GET /containers/{id}", s.inspectContainer)
	e("DELETE /containers/{id}", s.removeContainer)
	e("GET /containers/{id}/spec", s.containerSpec)
	e("POST /containers/{id}/recreate", s.recreateContainer)
	e("POST /containers/{id}/duplicate", s.duplicateContainer)
	e("POST /containers/{id}/update", s.updateContainer)
	e("POST /containers/{id}/rename", s.renameContainer)
	e("POST /containers/{id}/action/{action}", s.containerAction)
	e("GET /containers/{id}/logs", s.containerLogs)
	e("GET /containers/{id}/stats", s.containerStats)
	e("GET /containers/{id}/top", s.containerTop)
	e("GET /containers/{id}/exec", s.containerExec) // websocket
	e("GET /containers/{id}/files", s.containerFiles)
	e("GET /containers/{id}/download", s.containerDownload)
	e("POST /containers/{id}/commit", s.commitContainer)

	e("GET /images", s.listImages)
	e("GET /images/{id...}", s.inspectImage)
	e("POST /images/pull", s.pullImage)
	e("POST /images/push", s.pushImage)
	e("POST /images/tag", s.tagImage)
	e("POST /images/build", s.buildImage)
	e("POST /images/prune", s.pruneImages)
	e("DELETE /images/{id...}", s.removeImage)

	e("GET /networks", s.listNetworks)
	e("POST /networks", s.createNetwork)
	e("GET /networks/{id}", s.inspectNetwork)
	e("DELETE /networks/{id}", s.removeNetwork)
	e("POST /networks/{id}/connect", s.connectNetwork)
	e("POST /networks/{id}/disconnect", s.disconnectNetwork)
	e("POST /networks/prune", s.pruneNetworks)

	e("GET /volumes", s.listVolumes)
	e("POST /volumes", s.createVolume)
	e("GET /volumes/{name}", s.inspectVolume)
	e("DELETE /volumes/{name}", s.removeVolume)
	e("POST /volumes/prune", s.pruneVolumes)

	e("GET /jump", s.jump)
	e("POST /tunnels", s.openTunnel)

	e("GET /stacks", s.listStacks)
	e("POST /stacks", s.createStack)
	e("GET /stacks/{name}", s.getStack)
	e("PUT /stacks/{name}", s.updateStack)
	e("DELETE /stacks/{name}", s.deleteStack)
	e("POST /stacks/{name}/action/{action}", s.stackAction)
	e("POST /stacks/{name}/adopt", s.adoptStack)
	e("GET /stacks/{name}/logs", s.stackLogs)
	e("GET /stacks/{name}/merged", s.stackMerged)
	e("POST /stacks/{name}/merge", s.stackMerge)
	e("PUT /stacks/{name}/location", s.stackLocation)
}

func cut(pattern string) (method, path string, ok bool) {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == ' ' {
			return pattern[:i], pattern[i+1:], true
		}
	}
	return "", pattern, false
}
