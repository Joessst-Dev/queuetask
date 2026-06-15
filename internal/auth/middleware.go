package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/Joessst-Dev/queuetask/internal/config"
)

// Middleware returns a Fiber handler that validates the JWT from either an
// Authorization: Bearer header (API clients) or the queuetask_token cookie
// (browser UI). Returns a no-op passthrough when auth is disabled.
func Middleware(cfg config.AuthConfig) fiber.Handler {
	if !cfg.Enabled() {
		return func(c *fiber.Ctx) error { return c.Next() }
	}
	return func(c *fiber.Ctx) error {
		// The Tailwind CSS asset must be reachable without a token so the
		// login page can load. Exact match avoids prefix-based bypass shapes.
		if c.Path() == "/ui/static/tailwind.css" {
			return c.Next()
		}

		tokenStr := tokenFromRequest(c)
		if tokenStr == "" {
			return respondUnauthorized(c)
		}
		claims, err := VerifyToken(cfg.JWTSecret, tokenStr)
		if err != nil {
			return respondUnauthorized(c)
		}
		c.Locals("user", claims.Subject)
		return c.Next()
	}
}

func tokenFromRequest(c *fiber.Ctx) string {
	if h := c.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return c.Cookies(CookieName)
}

func respondUnauthorized(c *fiber.Ctx) error {
	// HTMX partial requests need HX-Redirect to trigger a full-page navigation.
	if c.Get("HX-Request") == "true" {
		c.Set("HX-Redirect", "/login")
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	if strings.Contains(c.Get("Accept"), "text/html") {
		return c.Redirect("/login", fiber.StatusFound)
	}
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
}
