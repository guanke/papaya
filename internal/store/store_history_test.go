package store

import (
	"os"
	"testing"
)

func TestStore_ChatHistory(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "papaya-test-history-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	s, err := New(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	userID := "999"
	data := []byte(`[{"role":"user","content":"hello"}]`)

	// 1. Initial Get should be empty
	got, err := s.GetChatHistory(userID)
	if err != nil {
		t.Fatalf("initial get failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty history, got %s", got)
	}

	// 2. Save
	if err := s.SaveChatHistory(userID, data); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// 3. Get Again
	got, err = s.GetChatHistory(userID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("expected %s, got %s", data, got)
	}
}
