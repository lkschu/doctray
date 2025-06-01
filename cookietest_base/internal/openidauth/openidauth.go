
package openidauth

import (
	"log"
	"net/http"
	"crypto/rand"
	"io"
	"fmt"

	"encoding/base64"
	"encoding/json"

	"github.com/gin-contrib/sessions"

	"golang.org/x/net/context"

	"github.com/gin-gonic/gin"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)





func randString(nByte int) (string, error) {
	b := make([]byte, nByte)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const (
	CookieName_AuthTemp = "session_auth"
	DefaultAuthenticatedURL = "/tralala"
)


type cookie_auth_content struct {
	State string		`json:"state"`
	Nonce string		`json:"nonce"`
	Redirect_to string	`json:"redir"`
}
func AuthFromJSON(cookie string) (cookie_auth_content, error) {
	var c cookie_auth_content
	err := json.Unmarshal([]byte(cookie),&c)
	if err != nil {
		fmt.Println("Error, can't unmarshall: ", err, "\n\n", cookie)
		return c, err
	}
	return c, nil
}
func (c cookie_auth_content) ToJSON() string {
	new_cookie_json, err := json.Marshal(c)
	if err != nil {
		panic("can't marshall cookie")
	}
	return string(new_cookie_json)
}



type AuthState struct {
	context		*context.Context
	provider	*oidc.Provider
	verifier	*oidc.IDTokenVerifier
	oauth2Conf  *oauth2.Config
	session_label_nonce string
	session_label_state string
	session_label_redirect string
	session_label_userid string
}
func (a AuthState) UserIDLabel() string {
	return a.session_label_userid
}
func NewAuthState(clientID string, clientSecret string, issuerUrl string, redirectURL string) AuthState{
	context := context.Background()

	provider, err := oidc.NewProvider(context, issuerUrl)
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
	return AuthState{provider: provider, verifier: verifier, oauth2Conf: &config, context: &context,
		session_label_nonce: "auth_nonce", session_label_state: "auth_state",
		session_label_redirect: "auth_redir", session_label_userid: "sub",}
}
func (state *AuthState) Login(ctx *gin.Context){
	previousURL := ctx.Request.RequestURI // Current URL
	if previousURL == "" {
		previousURL = DefaultAuthenticatedURL
	}

	w := ctx.Writer
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

	// auth_callback_cookie := cookie_auth_content{State: lstate, Nonce: nonce, Redirect_to: previousURL}
	// setCallbackCookieCtx(ctx, "session_auth", base64.RawURLEncoding.EncodeToString([]byte(auth_callback_cookie.ToJSON())))
	s := sessions.Default(ctx)
	s.Set(state.session_label_nonce, nonce)
	s.Set(state.session_label_state, lstate)
	s.Set(state.session_label_redirect, previousURL)
	s.Save()

	ctx.Redirect(http.StatusFound, state.oauth2Conf.AuthCodeURL(lstate, oidc.Nonce(nonce)))
}
func (state *AuthState) Logout() gin.HandlerFunc{
	// r := ctx.Request
	// _, err := ctx.Cookie("session")
	// if err != nil {
	// 	c := &http.Cookie{
	// 		Name:     "session",
	// 		Value:    "",
	// 		MaxAge:   -1,
	// 		Secure:   r.TLS != nil,
	// 		HttpOnly: true,
	// 	}
	// 	setLocalCookie(ctx, c)
	// }
	return func (ctx *gin.Context) {
		s := sessions.Default(ctx)
		s.Clear()
		// unsetLocalCookie(ctx,"session")
	}

}
func (state *AuthState) Ensure_loggedin() gin.HandlerFunc{
	return func(ctx *gin.Context) {
		w := ctx.Writer
		// session_cookie, err := ctx.Cookie("session")
		session := sessions.Default(ctx)
		uid := session.Get(state.session_label_userid)
		if uid == nil {
			fmt.Println("ENSURE_LOGGEDIN: Unauthorized")
			state.Login(ctx)
			return
		} else {
			fmt.Println("ENSURE_LOGGEDIN: Authorized!")
			w.Write([]byte("Authorized!"))
		}
	}

}
func (state *AuthState) Callback_handler() func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		// w http.ResponseWriter, r *http.Request
		r := ctx.Request
		w := ctx.Writer

		// state_cookie, err := r.Cookie("state")
		// state_cookie_json, err := r.Cookie("session_auth")

		session := sessions.Default(ctx)
		session_state := session.Get(state.session_label_state)
		if session_state == nil {
			http.Error(w, "no state found", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != session_state.(string) {
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

		session_nonce := session.Get(state.session_label_nonce)
		if session_state == nil {
			http.Error(w, "no state found", http.StatusBadRequest)
			return
		}
		if idToken.Nonce != session_nonce.(string) {
			http.Error(w, "nonce did not match", http.StatusBadRequest)
			return
		}

		redirection_url := DefaultAuthenticatedURL
		if redir := session.Get(state.session_label_redirect); redir != nil {
			redirection_url = redir.(string)
		}
		fmt.Println("Redirect to: ", redirection_url)

		resp := struct {
			OAuth2Token   *oauth2.Token
			IDTokenClaims *json.RawMessage // ID Token payload is just JSON.
		}{oauth2Token, new(json.RawMessage)}

		if err := idToken.Claims(&resp.IDTokenClaims); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}


		session.Set(state.session_label_userid, idToken.Subject)
		err = session.Save()
		if err != nil {
		    panic(err)
		}
		// new_cookie := cookie_content{}.NewFromUserID(idToken.Subject)
		// new_cookie.Authorized = true
		// new_cookie.ToCookie(ctx)


		// data, err := json.MarshalIndent(resp, "", "    ")
		// if err != nil {
		// 	http.Error(w, err.Error(), http.StatusInternalServerError)
		// 	return
		// }
		// w.Write(append(data, []byte("\nYada yada yada")...))
		// fmt.Println(string(data))
		// fmt.Println(idToken.Subject)
		ctx.Redirect(http.StatusFound, redirection_url)
	}
}
