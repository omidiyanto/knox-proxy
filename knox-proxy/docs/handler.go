package docs

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var OpenAPIYAML []byte

const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Knox API Documentation</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
  <style>
    body { margin: 0; padding: 0; background-color: #1c1c1d; }
    .swagger-ui .info h1, .swagger-ui .info h2, .swagger-ui .info h3, .swagger-ui .info h4, .swagger-ui .info h5, .swagger-ui .info p,	.swagger-ui .info a { color: #e8e8e8 !important; }
    .swagger-ui .scheme-container { background-color: #252528; box-shadow: none; border-bottom: 1px solid #3a3a3a; }
    .swagger-ui .topbar { display: none; }
    .swagger-ui { background-color: #1e1e1e; filter: invert(88%) hue-rotate(180deg); }
    .swagger-ui .opblock .opblock-summary-method { font-weight: bold; }
    .swagger-ui .scheme-container { filter: invert(100%) hue-rotate(180deg); }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.onload = () => {
      window.ui = SwaggerUIBundle({
        url: "/knox-swagger-api/openapi.yaml",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIBundle.SwaggerUIStandalonePreset
        ],
        layout: "BaseLayout",
      });
    };
  </script>
</body>
</html>`

// RegisterHandlers registers the Swagger UI and OpenAPI spec routes.
func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/knox-swagger-api", func(w http.ResponseWriter, r *http.Request) {
		// Redirect to trailing slash if not present to avoid relative path issues (though we use absolute above)
		if r.URL.Path == "/knox-swagger-api" {
			http.Redirect(w, r, "/knox-swagger-api/", http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(swaggerHTML))
	})

	mux.HandleFunc("/knox-swagger-api/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		// Enable CORS so it can be fetched if needed
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(OpenAPIYAML)
	})
}
