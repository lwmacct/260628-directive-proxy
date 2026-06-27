package service

import (
	"net/http"
)

type ProxyService struct {
	handler http.Handler
}

func NewProxyService(handler http.Handler) *ProxyService {
	return &ProxyService{
		handler: handler,
	}
}

func (s *ProxyService) Handler() http.Handler {
	if s == nil || s.handler == nil {
		return http.NotFoundHandler()
	}
	return s.handler
}
