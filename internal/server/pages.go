package server

import (
	"embed"
	"html/template"
	"log"
	"net/http"
)

//go:embed templates/*.html
var templatesFS embed.FS

var templates = template.Must(template.ParseFS(templatesFS, "templates/*.html"))

// pageVariant normalises the user-configurable variant name down to a known
// template family. Unknown values fall back to "default".
func pageVariant(variant string) string {
	switch variant {
	case "terminal", "minimal":
		return variant
	default:
		return "default"
	}
}

func writeStatusPage(w http.ResponseWriter, status int, variant, title, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	tplName := "status_" + pageVariant(variant) + ".html"
	data := struct {
		Title   string
		Message string
		Status  int
	}{
		Title:   title,
		Message: message,
		Status:  status,
	}

	if err := templates.ExecuteTemplate(w, tplName, data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func writePasswordPage(w http.ResponseWriter, variant, title, message, errMsg string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)

	tplName := "password_" + pageVariant(variant) + ".html"
	data := struct {
		Title   string
		Message string
		Error   string
	}{
		Title:   title,
		Message: message,
		Error:   errMsg,
	}

	if err := templates.ExecuteTemplate(w, tplName, data); err != nil {
		log.Printf("template error: %v", err)
	}
}
