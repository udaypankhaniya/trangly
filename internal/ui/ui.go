package ui

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

//go:embed static templates
var embeddedFiles embed.FS

// authCookie is the name of the browser cookie that holds the JWT.
// It is set by the frontend JS on login and cleared on logout.
// ui.go checks its presence (not cryptographic validity — that is the API layer's job)
// to decide whether to serve a protected page or redirect to login.
const authCookie = "sy_auth"

// renderer is the pre-parsed template renderer, initialised once in RegisterRoutes.
var renderer *Renderer

// RegisterRoutes registers the embedded UI routes onto the Fiber app.
//
//	GET /              → login page
//	GET /setup         → setup page — redirects to /dashboard if authenticated
//	GET /dashboard     → dashboard  — requires sy_auth cookie or redirect to /
//	GET /settings      → settings   — requires sy_auth cookie or redirect to /
//	GET /static/*      → static assets with correct MIME types
//	everything else    → 302 to /dashboard
func RegisterRoutes(app *fiber.App) {
	// Parse all templates once at startup.
	renderer = NewRenderer(embeddedFiles)

	// Static assets served via filesystem middleware.
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:       http.FS(embeddedFiles),
		PathPrefix: "static",
		Browse:     false,
	}))

	app.Get("/", serveLoginPage)
	app.Get("/login", serveLoginPage)
	app.Get("/setup", serveSetupPage)
	app.Get("/dashboard", serveProtectedPage("dashboard", "/dashboard", PageData{
		Title: "Dashboard", ActiveNav: "dashboard", AlpineData: "dashboardPage()",
	}))
	app.Get("/settings", serveProtectedPage("settings", "/settings", PageData{
		Title: "Settings", ActiveNav: "settings", AlpineData: "settingsPage()",
	}))
	app.Get("/profile", serveProtectedPage("profile", "/profile", PageData{
		Title: "Edit Profile", ActiveNav: "profile", AlpineData: "profilePage()",
	}))
	app.Get("/project", serveProtectedPage("project", "/project", PageData{
		Title: "Project", ActiveNav: "dashboard", AlpineData: "projectPage()",
	}))
	app.Get("/project/edit", serveProtectedPage("project_edit", "/project/edit", PageData{
		Title: "Edit Project", ActiveNav: "dashboard", AlpineData: "projectEditPage()",
	}))
	app.Get("/deployments", serveProtectedPage("deployments", "/deployments", PageData{
		Title: "Deployments", ActiveNav: "deployments", AlpineData: "deploymentsPage()",
	}))
	app.Get("/deployment", serveProtectedPage("deployment", "/deployment", PageData{
		Title: "Deployment", ActiveNav: "deployments", AlpineData: "deploymentPage()",
	}))

	// Catch-all: anything that doesn't match above → redirect to /dashboard.
	app.Use(func(c *fiber.Ctx) error {
		// Only catch non-API, non-webhook, non-static paths.
		// API and webhook routes are registered before UI routes in server.go,
		// so they are matched first by Fiber. This catch-all only fires for
		// unknown UI paths.
		return c.Redirect("/dashboard", fiber.StatusFound)
	})
}

func serveLoginPage(c *fiber.Ctx) error {
	return renderer.Render(c, "login", PageData{
		Title: "Sign In", AlpineData: "loginPage()",
	})
}

func serveSetupPage(c *fiber.Ctx) error {
	if hasAuthCookie(c) {
		return c.Redirect("/dashboard", fiber.StatusFound)
	}
	return renderer.Render(c, "setup", PageData{
		Title: "First-Run Setup", AlpineData: "setupPage()",
	})
}

func serveProtectedPage(page, nextPath string, data PageData) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !hasAuthCookie(c) {
			return c.Redirect("/?next="+nextPath, fiber.StatusFound)
		}
		return renderer.Render(c, page, data)
	}
}

// hasAuthCookie returns true if the sy_auth cookie is present and non-empty.
// Cryptographic validation happens in the API middleware; this is a presence-only check.
func hasAuthCookie(c *fiber.Ctx) bool {
	return c.Cookies(authCookie) != ""
}
