package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func credentialsDir() string {
	return filepath.Join(dataDir(), ".credentials")
}

func credentialsPath() string {
	return filepath.Join(credentialsDir(), "credentials.json")
}

func tokenPath() string {
	return filepath.Join(credentialsDir(), "token.json")
}

func oauthConfig() (*oauth2.Config, error) {
	b, err := os.ReadFile(credentialsPath())
	if err != nil {
		return nil, fmt.Errorf("missing credentials.json at %s\n  Download from Google Cloud Console → APIs & Services → Credentials", credentialsPath())
	}
	config, err := google.ConfigFromJSON(b, gmail.GmailModifyScope)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials.json: %w", err)
	}
	// Force loopback redirect for desktop flow
	config.RedirectURL = "http://localhost:8989/oauth/callback"
	return config, nil
}

func loadToken() (*oauth2.Token, error) {
	b, err := os.ReadFile(tokenPath())
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func saveToken(tok *oauth2.Token) error {
	os.MkdirAll(credentialsDir(), 0700)
	b, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tokenPath(), b, 0600)
}

func getGmailService() (*gmail.Service, error) {
	config, err := oauthConfig()
	if err != nil {
		return nil, err
	}

	tok, err := loadToken()
	if err != nil {
		return nil, fmt.Errorf("not authorized — run: gmail auth")
	}

	// TokenSource auto-refreshes expired tokens
	ctx := context.Background()
	ts := config.TokenSource(ctx, tok)

	// Try to get a valid token (triggers refresh if needed)
	newTok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("token expired and refresh failed — run: gmail auth")
	}
	// Persist refreshed token if it changed
	if newTok.AccessToken != tok.AccessToken {
		saveToken(newTok)
	}

	svc, err := gmail.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("gmail service init failed: %w", err)
	}
	return svc, nil
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", url).Start()
	case "linux":
		exec.Command("xdg-open", url).Start()
	default:
		fmt.Printf("  Open this URL in your browser:\n  %s\n", url)
	}
}

func runAuthFlow() error {
	config, err := oauthConfig()
	if err != nil {
		return err
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			fmt.Fprintf(w, "<h2>Error: no authorization code received</h2>")
			return
		}
		codeCh <- code
		fmt.Fprintf(w, "<h2>Authorized! You can close this tab.</h2>")
	})

	srv := &http.Server{Addr: ":8989", Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Printf("  Opening browser for authorization...\n")
	openBrowser(authURL)
	fmt.Printf("  If browser doesn't open, visit:\n  %s\n\n", authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		srv.Close()
		return err
	}

	srv.Close()

	ctx := context.Background()
	tok, err := config.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	if err := saveToken(tok); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	return nil
}
