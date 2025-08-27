package middleware

import "net/http"

type Middleware func(http.Handler) http.HandlerFunc

func CreateStack(middleware ...Middleware) Middleware {
	return func(next http.Handler) http.HandlerFunc {
		for i := len(middleware) - 1; i >= 0; i-- {
			next = middleware[i](next)
		}

		return next.ServeHTTP
	}
}
