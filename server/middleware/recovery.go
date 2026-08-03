package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/getcodescout/code_scout/pkg/cslog"
)

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				if w.Header().Get("Content-Type") == "" {
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				}
				w.WriteHeader(http.StatusInternalServerError)
				f := "PANIC: "
				stack := fmt.Sprint(f, err, ", stack trace:", '\n')
				cslog.Errorf(stack)
				fmt.Println(string(debug.Stack()))
				stack = fmt.Sprint(stack, string(debug.Stack()))
				w.Write([]byte(stack))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
