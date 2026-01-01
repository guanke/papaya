package store

import (
	"go.etcd.io/bbolt"
)

// SaveChatHistory persists the chat history for a user.
func (s *Store) SaveChatHistory(userID string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		// We use a separate bucket "history"
		// Key: userID (string)
		bucket := tx.Bucket([]byte(historyBucket))
		return bucket.Put([]byte(userID), data)
	})
}

// GetChatHistory retrieves the chat history for a user.
func (s *Store) GetChatHistory(userID string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var data []byte
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(historyBucket))
		v := bucket.Get([]byte(userID))
		if v != nil {
			// Copy data because val is only valid closely
			data = make([]byte, len(v))
			copy(data, v)
		}
		return nil
	})
	return data, err
}
