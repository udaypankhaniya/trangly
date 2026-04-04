package ui

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"

	"github.com/gofiber/fiber/v2"
)

// PageData holds the dynamic values injected into every page template.
type PageData struct {
	Title      string // Suffix for the <title> tag, e.g. "Dashboard"
	ActiveNav  string // Which navbar link is highlighted: "dashboard" | "deployments" | "settings" | ""
	AlpineData string // Alpine.js x-data attribute value, e.g. "dashboardPage()"
}

// Renderer holds pre-parsed, ready-to-execute templates for every UI page.
// Templates are parsed once at startup using Clone() to avoid namespace
// collisions when multiple page templates define the same block names.
type Renderer struct {
	pages map[string]*template.Template
}

// NewRenderer parses all templates from the embedded filesystem and returns a
// Renderer with each page fully composed (layout + partials + page overrides).
// It panics on any parse error so startup fails fast.
func NewRenderer(embeddedFS embed.FS) *Renderer {
	r := &Renderer{pages: make(map[string]*template.Template)}

	// Helper to resolve glob patterns against the embedded FS.
	mustGlob := func(t *template.Template, patterns ...string) *template.Template {
		for _, pattern := range patterns {
			matches, err := fs.Glob(embeddedFS, pattern)
			if err != nil {
				panic(fmt.Sprintf("template glob %q: %v", pattern, err))
			}
			for _, m := range matches {
				data, err := embeddedFS.ReadFile(m)
				if err != nil {
					panic(fmt.Sprintf("read template %q: %v", m, err))
				}
				_, err = t.Parse(string(data))
				if err != nil {
					panic(fmt.Sprintf("parse template %q: %v", m, err))
				}
			}
		}
		return t
	}

	// Parse base layout + partials into a reusable base set.
	baseSet := template.New("base")
	mustGlob(baseSet,
		"templates/partials/*.html",
		"templates/layouts/base.html",
	)

	// Parse auth base layout + partials into a reusable auth set.
	authSet := template.New("auth_base")
	mustGlob(authSet,
		"templates/partials/*.html",
		"templates/layouts/auth_base.html",
	)

	// Authenticated pages (use base layout).
	appPages := map[string]string{
		"dashboard":    "templates/pages/dashboard.html",
		"settings":     "templates/pages/settings.html",
		"profile":      "templates/pages/profile.html",
		"project":      "templates/pages/project.html",
		"project_edit": "templates/pages/project_edit.html",
		"deployments":  "templates/pages/deployments.html",
		"deployment":   "templates/pages/deployment.html",
	}
	for name, path := range appPages {
		cloned, err := baseSet.Clone()
		if err != nil {
			panic(fmt.Sprintf("clone base for %q: %v", name, err))
		}
		data, err := embeddedFS.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("read page %q: %v", path, err))
		}
		_, err = cloned.Parse(string(data))
		if err != nil {
			panic(fmt.Sprintf("parse page %q: %v", path, err))
		}
		r.pages[name] = cloned
	}

	// Auth pages (use auth_base layout).
	authPages := map[string]string{
		"login": "templates/pages/login.html",
		"setup": "templates/pages/setup.html",
	}
	for name, path := range authPages {
		cloned, err := authSet.Clone()
		if err != nil {
			panic(fmt.Sprintf("clone auth_base for %q: %v", name, err))
		}
		data, err := embeddedFS.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("read page %q: %v", path, err))
		}
		_, err = cloned.Parse(string(data))
		if err != nil {
			panic(fmt.Sprintf("parse page %q: %v", path, err))
		}
		r.pages[name] = cloned
	}

	return r
}

// Render executes the named page template with the given data and sends it
// as an HTML response via Fiber.
func (r *Renderer) Render(c *fiber.Ctx, page string, data PageData) error {
	t, ok := r.pages[page]
	if !ok {
		return c.SendStatus(fiber.StatusNotFound)
	}

	// Determine the entry-point template name.
	entryName := "base"
	switch page {
	case "login", "setup":
		entryName = "auth_base"
	}

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, entryName, data); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("template error")
	}

	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(buf.Bytes())
}
