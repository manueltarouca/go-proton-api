package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const sessionDir = ".proton-organizer"
const sessionFile = "session.json"

type SessionData struct {
	UID           string `json:"uid"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	SaltedKeyPass []byte `json:"salted_key_pass"`
}

func sessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, sessionDir, sessionFile), nil
}

func saveSession(data SessionData) error {
	path, err := sessionPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, b, 0600)
}

func loadSession() (SessionData, error) {
	path, err := sessionPath()
	if err != nil {
		return SessionData{}, err
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return SessionData{}, fmt.Errorf("no session file: %w", err)
	}

	var data SessionData
	if err := json.Unmarshal(b, &data); err != nil {
		return SessionData{}, fmt.Errorf("corrupt session file: %w", err)
	}

	if data.UID == "" || data.RefreshToken == "" {
		return SessionData{}, fmt.Errorf("incomplete session data")
	}

	return data, nil
}
