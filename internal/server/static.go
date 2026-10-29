package server

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/uiautodev/flashroute/frontend"
)

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || r.URL.Path == "/v1" || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	sub, err := fs.Sub(frontend.BuildFS, "build")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "frontend assets unavailable")
		return
	}
	file := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if file == "" || file == "." {
		file = "index.html"
	}
	if strings.HasPrefix(file, "../") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	data, err := fs.ReadFile(sub, file)
	if err != nil {
		if path.Ext(file) != "" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		file = "index.html"
		data, err = fs.ReadFile(sub, file)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "frontend is not built")
			return
		}
	}
	if ct := mime.TypeByExtension(path.Ext(file)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if file == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else if strings.HasPrefix(file, "_app/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	_, _ = w.Write(data)
}
