package auth_test

import (
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gofiber/fiber/v2"

	"github.com/Joessst-Dev/queuetask/internal/auth"
	"github.com/Joessst-Dev/queuetask/internal/config"
)

func newMiddlewareApp(cfg config.AuthConfig) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/ui/static/tailwind.css", func(c *fiber.Ctx) error {
		return c.SendString("css")
	})
	app.Use(auth.Middleware(cfg))
	app.Get("/protected", func(c *fiber.Ctx) error {
		user, _ := c.Locals("user").(string)
		return c.SendString(user)
	})
	return app
}

var _ = Describe("Middleware", func() {
	const secret = "test-middleware-secret"
	enabledCfg := config.AuthConfig{
		Username:    "admin",
		Password:    "secret",
		JWTSecret:   secret,
		TokenExpiry: time.Hour,
	}

	Describe("when auth is disabled (empty username/password)", func() {
		It("passes all requests through without checking tokens", func() {
			app := newMiddlewareApp(config.AuthConfig{})
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("when auth is enabled", func() {
		var app *fiber.App
		BeforeEach(func() { app = newMiddlewareApp(enabledCfg) })

		It("passes the Tailwind CSS asset without a token", func() {
			req := httptest.NewRequest(http.MethodGet, "/ui/static/tailwind.css", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("returns 401 JSON for requests with no token and no Accept: text/html", func() {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(resp.Header.Get("Content-Type")).To(ContainSubstring("application/json"))
		})

		It("redirects browser requests (Accept: text/html) to /login", func() {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Accept", "text/html")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			Expect(resp.Header.Get("Location")).To(Equal("/login"))
		})

		It("sends HX-Redirect for HTMX requests", func() {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("HX-Request", "true")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(resp.Header.Get("HX-Redirect")).To(Equal("/login"))
		})

		It("returns 401 for a malformed token", func() {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer not.a.valid.token")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("allows requests with a valid Bearer token and sets user local", func() {
			tok, err := auth.IssueToken(secret, "admin", time.Hour)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("allows requests with a valid cookie token", func() {
			tok, err := auth.IssueToken(secret, "admin", time.Hour)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("prefers the Bearer header over the cookie when both are present", func() {
			validTok, err := auth.IssueToken(secret, "admin", time.Hour)
			Expect(err).NotTo(HaveOccurred())

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer "+validTok)
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "bad-cookie-token"})
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})
})
