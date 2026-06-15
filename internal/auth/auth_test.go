package auth_test

import (
	"encoding/base64"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/auth"
)

var _ = Describe("IssueToken / VerifyToken", func() {
	const secret = "test-secret-32-bytes-long-enough!"
	const username = "admin"

	It("round-trips a valid token", func() {
		tok, err := auth.IssueToken(secret, username, time.Hour)
		Expect(err).NotTo(HaveOccurred())
		Expect(tok).NotTo(BeEmpty())

		claims, err := auth.VerifyToken(secret, tok)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.Subject).To(Equal(username))
		Expect(claims.ExpiresAt.Time).To(BeTemporally(">", time.Now()))
	})

	It("rejects a token signed with a different secret", func() {
		tok, err := auth.IssueToken(secret, username, time.Hour)
		Expect(err).NotTo(HaveOccurred())

		_, err = auth.VerifyToken("wrong-secret", tok)
		Expect(err).To(HaveOccurred())
	})

	It("rejects an expired token", func() {
		tok, err := auth.IssueToken(secret, username, -time.Second)
		Expect(err).NotTo(HaveOccurred())

		_, err = auth.VerifyToken(secret, tok)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token with the 'none' algorithm (alg-confusion attack)", func() {
		// Craft a raw JWT with alg:none — valid-looking header + payload, no signature.
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"admin","exp":9999999999}`))
		noneToken := header + "." + payload + "."

		_, err := auth.VerifyToken(secret, noneToken)
		Expect(err).To(HaveOccurred())
	})
})
