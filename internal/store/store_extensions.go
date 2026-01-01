package store

import (
    "encoding/json"
    "fmt"
    
    "go.etcd.io/bbolt"
)

// SetPersona saves the user's persona (system prompt customization)
func (s *Store) SetPersona(id string, persona string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		key := []byte(id)
		data := bucket.Get(key)
		if data == nil {
			return fmt.Errorf("user %s not found", id)
		}
		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}
		user.Persona = persona
		return saveUser(bucket, &user)
	})
}

// SetMediaTags updates the tags for a media item.
func (s *Store) SetMediaTags(id string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(imagesBucket))
		data := bucket.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("media %s not found", id)
		}

		var m Media
		if err := json.Unmarshal(data, &m); err != nil {
			return err
		}
		m.Tags = tags

		newData, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(id), newData)
	})
}

// SetVisionEnabled sets the global vision enabled status.
func (s *Store) SetVisionEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(settingsBucket))
		val := "0"
		if enabled {
			val = "1"
		}
		return bucket.Put([]byte(visionKey), []byte(val))
	})
}

// GetVisionEnabled returns the global vision enabled status.
func (s *Store) GetVisionEnabled() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var enabled bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(settingsBucket))
		value := bucket.Get([]byte(visionKey))
		if value != nil && string(value) == "1" {
			enabled = true
		}
		return nil
	})
	return enabled, err
}
