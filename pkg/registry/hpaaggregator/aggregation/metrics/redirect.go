package metrics

import (
	"github.com/emicklei/go-restful/v3"
)

func addNewRedirect(curPath, targetPath string, s *restful.Container) {
	ws := new(restful.WebService)
	ws.Path(curPath).Route(ws.GET("/").To(
		func(request *restful.Request, response *restful.Response) {
			request.Request.URL.Path = targetPath
			s.ServeHTTP(response.ResponseWriter, request.Request)
		},
	))
	s.Add(ws)
}
