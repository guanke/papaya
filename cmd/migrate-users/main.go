// Command migrate-users converts old-format users (int64 ID) to new format (string ID).
// This is a one-time migration tool. Run it with the database file path as argument.
//
// Usage: go run cmd/migrate-users/main.go /path/to/data.db
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"go.etcd.io/bbolt"
)

const usersBucket = "users"

// OldUser represents the old format with int64 ID.
type OldUser struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Points      int    `json:"points"`
	LastCheckin string `json:"last_checkin"`
	IsAdmin     bool   `json:"is_admin"`
	DisplayName string `json:"display_name"`
	Persona     string `json:"persona,omitempty"`
}

// NewUser represents the new format with string ID.
type NewUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Points      int    `json:"points"`
	LastCheckin string `json:"last_checkin"`
	IsAdmin     bool   `json:"is_admin"`
	DisplayName string `json:"display_name"`
	Persona     string `json:"persona,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run cmd/migrate-users/main.go /path/to/data.db")
	}

	dbPath := os.Args[1]
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Fatalf("Database file not found: %s", dbPath)
	}

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	var migrated, skipped, failed int

	err = db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %q not found", usersBucket)
		}

		// Collect all keys first to avoid modifying bucket while iterating
		type migration struct {
			oldKey []byte
			newKey []byte
			data   []byte
		}
		var migrations []migration

		err := bucket.ForEach(func(k, v []byte) error {
			// Try to parse as new format first
			var newUser NewUser
			if err := json.Unmarshal(v, &newUser); err == nil && newUser.ID != "" {
				// Check if the ID looks like a string (quoted in JSON)
				// by checking if it matches the key
				if string(k) == newUser.ID {
					skipped++
					log.Printf("Skipping user (already migrated): key=%s", string(k))
					return nil
				}
			}

			// Try parsing as old format
			var oldUser OldUser
			if err := json.Unmarshal(v, &oldUser); err != nil {
				failed++
				log.Printf("Failed to parse user data: key=%s, error=%v", string(k), err)
				return nil
			}

			// Convert to new format
			newUser = NewUser{
				ID:          fmt.Sprintf("%d", oldUser.ID),
				Username:    oldUser.Username,
				Points:      oldUser.Points,
				LastCheckin: oldUser.LastCheckin,
				IsAdmin:     oldUser.IsAdmin,
				DisplayName: oldUser.DisplayName,
				Persona:     oldUser.Persona,
			}

			newData, err := json.Marshal(newUser)
			if err != nil {
				failed++
				log.Printf("Failed to marshal new user: key=%s, error=%v", string(k), err)
				return nil
			}

			newKey := []byte(newUser.ID)
			migrations = append(migrations, migration{
				oldKey: k,
				newKey: newKey,
				data:   newData,
			})

			return nil
		})
		if err != nil {
			return err
		}

		// Apply migrations
		for _, m := range migrations {
			// Delete old key if different from new key
			if string(m.oldKey) != string(m.newKey) {
				if err := bucket.Delete(m.oldKey); err != nil {
					failed++
					log.Printf("Failed to delete old key: %s, error=%v", string(m.oldKey), err)
					continue
				}
			}

			// Put new data with new key
			if err := bucket.Put(m.newKey, m.data); err != nil {
				failed++
				log.Printf("Failed to save new user: key=%s, error=%v", string(m.newKey), err)
				continue
			}

			migrated++
			log.Printf("Migrated user: %s -> %s", string(m.oldKey), string(m.newKey))
		}

		return nil
	})

	if err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	log.Printf("Migration complete: migrated=%d, skipped=%d, failed=%d", migrated, skipped, failed)
}
