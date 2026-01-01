package store

import (
	"os"
	"testing"
)

func TestStore_Persona(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "papaya-test-persona-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	s, err := New(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 1. Create User
	userID := "1001"
	_, err = s.GetOrCreateUser(userID, "alice", "Alice")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// 2. Set Persona
	persona := "You are a pirate."
	if err := s.SetPersona(userID, persona); err != nil {
		t.Fatalf("failed to set persona: %v", err)
	}

	// 3. Verify Persistence
	u, err := s.GetOrCreateUser(userID, "", "")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if u.Persona != persona {
		t.Errorf("expected persona %q, got %q", persona, u.Persona)
	}
}

func TestStore_VisionSettings(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "papaya-test-vision-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	s, err := New(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Default should be disabled (false)
	enabled, err := s.GetVisionEnabled()
	if err != nil {
		t.Fatalf("failed to get vision enabled: %v", err)
	}
	if enabled {
		t.Error("expected vision disabled by default")
	}

	// Enable it
	if err := s.SetVisionEnabled(true); err != nil {
		t.Fatalf("failed to set vision enabled: %v", err)
	}

	enabled, err = s.GetVisionEnabled()
	if err != nil {
		t.Fatalf("failed to get vision enabled: %v", err)
	}
	if !enabled {
		t.Error("expected vision enabled")
	}
}

func TestStore_MediaTags(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "papaya-test-tags-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	s, err := New(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 1. Save Media
    fileID := "file123"
	if err := s.SaveMedia(fileID, "photo", "caption", "123"); err != nil {
		t.Fatalf("failed to save media: %v", err)
	}
    
    // Get the ID (we can't easily know the ID since it is timestamp based, unless we check ListMedia)
    list, err := s.ListMedia(1, 0)
    if err != nil || len(list) == 0 {
        t.Fatal("failed to list media")
    }
    mediaID := list[0].ID

	// 2. Set Tags
	tags := []string{"sunset", "mountain"}
	if err := s.SetMediaTags(mediaID, tags); err != nil {
		t.Fatalf("failed to set tags: %v", err)
	}

	// 3. Verify
	m, err := s.GetMedia(mediaID)
	if err != nil {
		t.Fatalf("failed to get media: %v", err)
	}
	if len(m.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(m.Tags))
	}
	if m.Tags[0] != "sunset" {
		t.Errorf("expected tag sunset, got %q", m.Tags[0])
	}
}
