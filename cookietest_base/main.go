/*
This is an example application to demonstrate parsing an ID Token.
*/
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)


func randString(nByte int) (string, error) {
	b := make([]byte, nByte)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func setCallbackCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   int(time.Minute.Seconds()),
		Secure:   r.TLS != nil,
		HttpOnly: true,
	}
	http.SetCookie(w, c)
}


type AuthState struct {
	context		*context.Context
	provider	*oidc.Provider
	verifier	*oidc.IDTokenVerifier
	oauth2Conf  *oauth2.Config
}
func NewAuthState(ctx context.Context, clientID string, clientSecret string, issuerUrl string, redirectURL string) AuthState{
	provider, err := oidc.NewProvider(ctx, issuerUrl)
	if err != nil {
		log.Fatal(err)
	}
	oidcConfig := &oidc.Config{
		ClientID: clientID,
	}
	verifier := provider.Verifier(oidcConfig)
	config := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURL,
		// Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		Scopes:       []string{oidc.ScopeOpenID, "sub", "email"},
	}
	return AuthState{provider: provider, verifier: verifier, oauth2Conf: &config, context: &ctx}
}
func (state *AuthState) login(w http.ResponseWriter, r *http.Request){
	lstate, err := randString(16)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	nonce, err := randString(16)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	setCallbackCookie(w, r, "state", lstate)
	setCallbackCookie(w, r, "nonce", nonce)

	http.Redirect(w, r, state.oauth2Conf.AuthCodeURL(lstate, oidc.Nonce(nonce)), http.StatusFound)
}
func (state *AuthState) logout(w http.ResponseWriter, r *http.Request){
}
func (state *AuthState) ensure_loggedin(w http.ResponseWriter, r *http.Request){
	auth_cookie, err := r.Cookie("session")
	if err != nil || auth_cookie.Value != "Y" {
		fmt.Println("Unauthorized")
		state.login(w,r)
		return
	} else {
		fmt.Println("Authorized!")
		w.Write([]byte("Authorized!"))
	}

}
func (state *AuthState) callback_handler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		state_cookie, err := r.Cookie("state")
		if err != nil {
			http.Error(w, "state not found", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != state_cookie.Value {
			http.Error(w, "state did not match", http.StatusBadRequest)
			return
		}

		oauth2Token, err := state.oauth2Conf.Exchange(*state.context, r.URL.Query().Get("code"))
		if err != nil {
			http.Error(w, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "No id_token field in oauth2 token.", http.StatusInternalServerError)
			return
		}
		idToken, err := state.verifier.Verify(*state.context, rawIDToken)
		if err != nil {
			http.Error(w, "Failed to verify ID Token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		nonce_cookie, err := r.Cookie("nonce")
		if err != nil {
			http.Error(w, "nonce not found", http.StatusBadRequest)
			return
		}
		if idToken.Nonce != nonce_cookie.Value {
			http.Error(w, "nonce did not match", http.StatusBadRequest)
			return
		}

		// oauth2Token.AccessToken = "*REDACTED*"

		resp := struct {
			OAuth2Token   *oauth2.Token
			IDTokenClaims *json.RawMessage // ID Token payload is just JSON.
		}{oauth2Token, new(json.RawMessage)}

		if err := idToken.Claims(&resp.IDTokenClaims); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data, err := json.MarshalIndent(resp, "", "    ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		c := &http.Cookie{
			Name:     "session",
			Value:    "Y",
			MaxAge:   int(time.Minute.Seconds()),
			Secure:   r.TLS != nil,
			HttpOnly: true,
		}
		http.SetCookie(w, c)

		w.Write(data)
	}
}

// TODO: next steps:
//  - commit
//  - port to gin gonic
//  - secure and cleanup cookies
//  -> middleware module
//  - then move to doctray

func main() {
	ctx := context.Background()
	clientID     := os.Getenv("DOCTRAY_CLIENTID")
	clientSecret := os.Getenv("DOCTRAY_CLIENTSECRET")
	issuerUrl    := "https://auth.lukasschumann.de/application/o/test-app/"
	redirectURL  := "http://127.0.0.1:5555/callback"
	auth_state := NewAuthState(ctx, clientID, clientSecret, issuerUrl, redirectURL)


	fmt.Println("hello")
	http.HandleFunc("/", auth_state.ensure_loggedin)

	http.HandleFunc("/callback", auth_state.callback_handler())

	log.Printf("listening on http://%s/", "127.0.0.1:5555")
	log.Fatal(http.ListenAndServe("127.0.0.1:5555", nil))
}
