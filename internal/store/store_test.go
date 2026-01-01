package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStore_ListUsersParameters(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "papaya_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 1. Empty list
	users, err := s.ListUsers(10, 0)
	if err != nil {
		t.Fatalf("ListUsers empty failed: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("Expected 0 users, got %d", len(users))
	}

	// 2. Populate 25 users
	total := 25
	for i := 1; i <= total; i++ {
		uid := int64(100 + i) // IDs 101..125
		_, err := s.GetOrCreateUser(uid, fmt.Sprintf("user%d", i), fmt.Sprintf("User %d", i))
		if err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}

	// 3. Test pagination
	tests := []struct {
		name    string
		limit   int
		offset  int
		wantLen int
		startID int64
		endID   int64
	}{
		{"Page 1", 10, 0, 10, 101, 110},
		{"Page 2", 10, 10, 10, 111, 120},
		{"Page 3", 10, 20, 5, 121, 125},
		{"Page 4 (Empty)", 10, 30, 0, 0, 0},
		{"Large Limit", 100, 0, 25, 101, 125},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ListUsers(tt.limit, tt.offset)
			if err != nil {
				t.Fatalf("ListUsers failed: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("got length %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen > 0 {
				if got[0].ID != tt.startID {
					t.Errorf("first user ID got %d, want %d", got[0].ID, tt.startID)
				}
				if got[len(got)-1].ID != tt.endID {
					t.Errorf("last user ID got %d, want %d", got[len(got)-1].ID, tt.endID)
				}
			}
		})
	}
}

func TestStore_Media(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "papaya_media_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 1. Save Media
    err = s.SaveMedia("file_123", "photo", "caption 1", 1001)
    if err != nil {
        t.Fatalf("SaveMedia failed: %v", err)
    }
    
    // 2. Get Random
    m, err := s.GetRandomMedia()
    if err != nil {
        t.Fatalf("GetRandomMedia failed: %v", err)
    }
    if m == nil {
        t.Fatal("GetRandomMedia returned nil")
    }
    if m.FileID != "file_123" {
        t.Errorf("got fileID %s, want file_123", m.FileID)
    }
    
    // 3. List
    list, err := s.ListMedia(10, 0)
    if err != nil {
        t.Fatalf("ListMedia failed: %v", err)
    }
    if len(list) != 1 {
        t.Errorf("got list len %d, want 1", len(list))
    }
    
    // 4. Delete
    err = s.DeleteMedia(m.ID)
    if err != nil {
        t.Fatalf("DeleteMedia failed: %v", err)
    }
    
    list, err = s.ListMedia(10, 0)
    if len(list) != 0 {
        t.Errorf("got list len %d after delete, want 0", len(list))
    }
    list, err = s.ListMedia(10, 0)
    if len(list) != 0 {
        t.Errorf("got list len %d after delete, want 0", len(list))
    }
}

func TestStore_MediaR2AndCount(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "papaya_media_r2_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

    // 1. Initial count
    count, err := s.CountMedia()
    if err != nil {
        t.Fatalf("CountMedia failed: %v", err)
    }
    if count != 0 {
        t.Errorf("initial count %d, want 0", count)
    }

    // 2. Add item
    err = s.SaveMedia("f1", "photo", "c1", 1)
    if err != nil {
        t.Fatal(err)
    }

    count, err = s.CountMedia()
    if count != 1 {
        t.Errorf("count %d, want 1", count)
    }

    // 3. Set R2 Key
    // Need to find ID first
    list, _ := s.ListMedia(1, 0)
    id := list[0].ID
    
    err = s.SetMediaR2(id, "r2_key_123")
    if err != nil {
        t.Fatalf("SetMediaR2 failed: %v", err)
    }

    // 4. Verify R2 Key
    list, _ = s.ListMedia(1, 0)
    if list[0].R2Key != "r2_key_123" {
        t.Errorf("got R2Key %s, want r2_key_123", list[0].R2Key)
    }
}
