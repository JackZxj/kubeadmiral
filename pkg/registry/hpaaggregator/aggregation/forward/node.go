package forward

import (
	"net/http"
)

func NewNodeHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("Hello, World!"))
	})
}
