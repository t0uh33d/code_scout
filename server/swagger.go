package server

import (
	_ "embed"
	"net/http"

	"github.com/gorilla/mux"
)

//go:embed docs/openapi.yaml
var openapiYAML []byte

const swaggerIndexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <title>Code Scout API — Swagger</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" crossorigin="anonymous"/>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" crossorigin="anonymous"></script>
<script>
  window.onload = function () {
    window.ui = SwaggerUIBundle({
      url: "/swagger/openapi.yaml",
      dom_id: '#swagger-ui',
      presets: [SwaggerUIBundle.presets.apis],
    });
  };
</script>
</body>
</html>`

func registerSwaggerRoutes(router *mux.Router) {
	router.HandleFunc("/swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/", http.StatusMovedPermanently)
	}).Methods(http.MethodGet)

	router.HandleFunc("/swagger/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(swaggerIndexHTML))
	}).Methods(http.MethodGet)

	router.HandleFunc("/swagger/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(openapiYAML)
	}).Methods(http.MethodGet)
}
