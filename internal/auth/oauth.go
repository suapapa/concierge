package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/suapapa/concierge/internal/config"
	"github.com/suapapa/concierge/internal/store"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// OAuth handles Google login redirects and callback.
type OAuth struct {
	cfg    *config.Config
	store  *store.Store
	oauth2 *oauth2.Config
	ver    *oidc.IDTokenVerifier
}

// NewOAuth builds Google OAuth wiring. cfg must have DatabaseURL and Google fields set.
func NewOAuth(ctx context.Context, cfg *config.Config, st *store.Store) (*OAuth, error) {
	if cfg == nil || st == nil {
		return nil, fmt.Errorf("oauth: nil config or store")
	}
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.GoogleClientID})
	o := &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.OAuthRedirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		Endpoint:     google.Endpoint,
	}
	return &OAuth{cfg: cfg, store: st, oauth2: o, ver: verifier}, nil
}

// Start redirects the browser to Google with a signed CSRF state parameter.
func (o *OAuth) Start(c *gin.Context) {
	state, err := makeSignedState(o.cfg.SessionSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "random state"})
		return
	}
	url := o.oauth2.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusFound, url)
}

// Callback completes the OAuth code exchange, verifies the ID token, and sets the session cookie.
func (o *OAuth) Callback(c *gin.Context) {
	state := c.Query("state")
	if state == "" || !verifySignedState(o.cfg.SessionSecret, state) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid oauth state"})
		return
	}

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code"})
		return
	}
	tok, err := o.oauth2.Exchange(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token exchange failed"})
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id_token"})
		return
	}
	idTok, err := o.ver.Verify(c.Request.Context(), rawID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid id_token"})
		return
	}
	var claims struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := idTok.Claims(&claims); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "claims"})
		return
	}
	if claims.Sub == "" || claims.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing sub or email"})
		return
	}
	adminSet := make(map[string]struct{})
	for _, e := range o.cfg.BootstrapAdminEmails {
		adminSet[strings.ToLower(strings.TrimSpace(e))] = struct{}{}
	}
	u, err := o.store.UpsertGoogleUser(c.Request.Context(), claims.Sub, claims.Email, claims.Name, claims.Picture, adminSet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user upsert failed"})
		return
	}
	ttl := o.cfg.SessionTTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	rawSess, err := o.store.CreateSession(c.Request.Context(), u.ID, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session failed"})
		return
	}
	maxAge := int(ttl.Seconds())
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    hex.EncodeToString(rawSess),
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   o.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	redir := safeRedirect(o.cfg.PostLoginRedirect)
	c.Redirect(http.StatusFound, redir)
}

func safeRedirect(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") {
		return p
	}
	return "/"
}

func makeSignedState(secret string) (string, error) {
	var rnd [16]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write(rnd[:])
	sig := m.Sum(nil)
	return hex.EncodeToString(rnd[:]) + "." + hex.EncodeToString(sig), nil
}

func verifySignedState(secret, state string) bool {
	parts := strings.Split(state, ".")
	if len(parts) != 2 {
		return false
	}
	rnd, err := hex.DecodeString(parts[0])
	if err != nil || len(rnd) != 16 {
		return false
	}
	wantSig, err := hex.DecodeString(parts[1])
	if err != nil || len(wantSig) != sha256.Size {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write(rnd)
	return hmac.Equal(m.Sum(nil), wantSig)
}
