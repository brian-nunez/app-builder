package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"github.com/brian-nunez/app-builder/internal/platform"
	"github.com/brian-nunez/baccess"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"net/http"
	"strings"
	"time"
)

type identity struct {
	Subject string `json:"subject"`
	Role    string `json:"role"`
	Expires int64  `json:"expires"`
}
type auth struct {
	cipher   *platform.Cipher
	token    string
	secure   bool
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}
type actorKey struct{}

func (a *auth) setCookie(w http.ResponseWriter, name string, value any, maxAge int) error {
	raw, err := a.cipher.Seal(value, name)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: base64.RawURLEncoding.EncodeToString(raw), Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
	return nil
}
func (a *auth) readCookie(r *http.Request, name string, v any) error {
	cookie, err := r.Cookie(name)
	if err != nil {
		return err
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return err
	}
	return a.cipher.Open(raw, name, v)
}
func (a *auth) login(w http.ResponseWriter, r *http.Request) {
	if a.oauth != nil {
		state := randomToken()
		verifier := oauth2.GenerateVerifier()
		if err := a.setCookie(w, "login", map[string]any{"state": state, "verifier": verifier, "expires": time.Now().Add(5 * time.Minute).Unix()}, 300); err != nil {
			fail(w, err)
			return
		}
		http.Redirect(w, r, a.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "OIDC is not configured", 400)
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	if a.token == "" || subtle.ConstantTimeCompare([]byte(body.Token), []byte(a.token)) != 1 {
		http.Error(w, "Invalid access token", 401)
		return
	}
	if err := a.setCookie(w, "session", identity{"local-operator", "admin", time.Now().Add(8 * time.Hour).Unix()}, 28800); err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, map[string]bool{"authenticated": true})
}
func (a *auth) callback(w http.ResponseWriter, r *http.Request) {
	if a.oauth == nil {
		http.NotFound(w, r)
		return
	}
	var login struct {
		State    string `json:"state"`
		Verifier string `json:"verifier"`
		Expires  int64  `json:"expires"`
	}
	if a.readCookie(r, "login", &login) != nil || login.Expires < time.Now().Unix() || login.State == "" || login.State != r.URL.Query().Get("state") {
		http.Error(w, "Invalid login state", 401)
		return
	}
	token, err := a.oauth.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(login.Verifier))
	if err != nil {
		http.Error(w, "Login failed", 401)
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "Missing ID token", 401)
		return
	}
	verified, err := a.verifier.Verify(r.Context(), raw)
	if err != nil {
		http.Error(w, "Invalid ID token", 401)
		return
	}
	var claims struct {
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
	}
	if err = verified.Claims(&claims); err != nil {
		http.Error(w, "Invalid identity", 401)
		return
	}
	role := ""
	for _, v := range claims.RealmAccess.Roles {
		if v == "workflow-admin" {
			role = "admin"
			break
		}
		if v == "workflow-editor" {
			role = "editor"
		}
		if v == "workflow-viewer" && role == "" {
			role = "viewer"
		}
	}
	if role == "" {
		http.Error(w, "Workflow role required", 403)
		return
	}
	if err = a.setCookie(w, "session", identity{verified.Subject, role, time.Now().Add(8 * time.Hour).Unix()}, 28800); err != nil {
		fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "login", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secure})
	http.Redirect(w, r, "/", http.StatusFound)
}
func (a *auth) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var user identity
		if a.readCookie(r, "session", &user) != nil || user.Expires < time.Now().Unix() {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if a.token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) == 1 {
				user = identity{"local-operator", "admin", time.Now().Add(time.Minute).Unix()}
			} else {
				http.Error(w, "Authentication required", 401)
				return
			}
		}
		action := "read"
		if r.Method != "GET" {
			action = "write"
		}
		if strings.HasSuffix(r.URL.Path, "/bindings") {
			action = "admin"
		}
		policy := baccess.Predicate[baccess.AccessRequest[identity, string]](func(req baccess.AccessRequest[identity, string]) bool {
			return req.Subject.Role == "admin" || (req.Action == "read" && (req.Subject.Role == "viewer" || req.Subject.Role == "editor")) || (req.Action == "write" && req.Subject.Role == "editor")
		})
		if !policy(baccess.AccessRequest[identity, string]{Subject: user, Action: action}) {
			http.Error(w, "Forbidden", 403)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, user.Subject)))
	})
}
func randomToken() string {
	return rand.Text() + rand.Text()
}
func tokenHash(v string) string    { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func actor(r *http.Request) string { v, _ := r.Context().Value(actorKey{}).(string); return v }
