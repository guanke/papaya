package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

const (
	usersBucket    = "users"
	settingsBucket = "settings"
	modelKey       = "openai_model"
)

// User represents a Telegram user state.
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Points      int    `json:"points"`
	LastCheckin string `json:"last_checkin"`
	IsAdmin     bool   `json:"is_admin"`
	DisplayName string `json:"display_name"`
}

// Store persists user data and settings to BoltDB.
type Store struct {
	db   *bbolt.DB
	mu   sync.Mutex
	zone *time.Location
}

// New initializes the store with the provided file path.
func New(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, zone: time.FixedZone("CST-8", 8*3600)}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		if _, e := tx.CreateBucketIfNotExists([]byte(usersBucket)); e != nil {
			return e
		}
		if _, e := tx.CreateBucketIfNotExists([]byte(settingsBucket)); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases database resources.
func (s *Store) Close() error {
	return s.db.Close()
}

// GetOrCreateUser fetches a user or creates it with defaults.
func (s *Store) GetOrCreateUser(id int64, username, displayName string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		data := bucket.Get(itob(id))
		if data != nil {
			if err := json.Unmarshal(data, &user); err != nil {
				return err
			}
			if username != "" {
				user.Username = username
			}
			if displayName != "" {
				user.DisplayName = displayName
			}
			return saveUser(bucket, &user)
		}
		user = User{ID: id, Username: username, DisplayName: displayName, Points: 0}
		return saveUser(bucket, &user)
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// AddPoints adjusts a user's points by delta.
func (s *Store) AddPoints(id int64, delta int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		data := bucket.Get(itob(id))
		if data == nil {
			return fmt.Errorf("user %d not found", id)
		}
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}
		user.Points += delta
		return saveUser(bucket, &user)
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// SetPoints sets a user's points to a specific value.
func (s *Store) SetPoints(id int64, points int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		data := bucket.Get(itob(id))
		if data == nil {
			return fmt.Errorf("user %d not found", id)
		}
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}
		user.Points = points
		return saveUser(bucket, &user)
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// PromoteAdmin grants admin role to a user.
func (s *Store) PromoteAdmin(id int64) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		data := bucket.Get(itob(id))
		if data == nil {
			return fmt.Errorf("user %d not found", id)
		}
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}
		user.IsAdmin = true
		return saveUser(bucket, &user)
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// CheckIn processes a daily check-in and returns gained points or an error.
func (s *Store) CheckIn(id int64, reward int) (int, *User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	var gained int
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		data := bucket.Get(itob(id))
		if data == nil {
			return errors.New("user not found")
		}
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}
		now := time.Now().In(s.zone)
		day := now.Format("2006-01-02")
		if user.LastCheckin == day {
			gained = 0
			return nil
		}
		user.LastCheckin = day
		user.Points += reward
		gained = reward
		return saveUser(bucket, &user)
	})
	if err != nil {
		return 0, nil, err
	}
	return gained, &user, nil
}

// ListUsers retrieves all users sorted by ID.
func (s *Store) ListUsers() ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var users []User
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		return bucket.ForEach(func(_, v []byte) error {
			var u User
			if err := json.Unmarshal(v, &u); err != nil {
				return err
			}
			users = append(users, u)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	return users, nil
}

// SetModel updates the stored OpenAI model name.
func (s *Store) SetModel(model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(settingsBucket))
		return bucket.Put([]byte(modelKey), []byte(model))
	})
}

// GetModel returns the stored model or empty string when unset.
func (s *Store) GetModel() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var model string
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(settingsBucket))
		value := bucket.Get([]byte(modelKey))
		if value != nil {
			model = string(value)
		}
		return nil
	})
	return model, err
}

func itob(v int64) []byte {
	return []byte(fmt.Sprintf("%d", v))
}

func saveUser(bucket *bbolt.Bucket, user *User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return bucket.Put(itob(user.ID), data)
}
