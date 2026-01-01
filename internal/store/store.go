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
	imagesBucket   = "images"
	subsBucket     = "subs"
	historyBucket  = "history"
	modelKey       = "openai_model"
	rateLimitKey   = "chat_rate_limit"
	visionKey      = "vision_enabled"
)

// User represents a Telegram user state.
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Points      int    `json:"points"`
	LastCheckin string `json:"last_checkin"`
	IsAdmin     bool   `json:"is_admin"`
	DisplayName string `json:"display_name"`
	Persona     string `json:"persona,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling to handle backward compatibility.
// Old format had ID as int64, new format has ID as string.
func (u *User) UnmarshalJSON(data []byte) error {
	// Try new format first (string ID)
	type UserAlias User
	var alias UserAlias
	if err := json.Unmarshal(data, &alias); err == nil && alias.ID != "" {
		*u = User(alias)
		return nil
	}

	// Fall back to old format (int64 ID)
	type OldUser struct {
		ID          int64  `json:"id"`
		Username    string `json:"username"`
		Points      int    `json:"points"`
		LastCheckin string `json:"last_checkin"`
		IsAdmin     bool   `json:"is_admin"`
		DisplayName string `json:"display_name"`
		Persona     string `json:"persona,omitempty"`
	}
	var old OldUser
	if err := json.Unmarshal(data, &old); err != nil {
		return err
	}
	u.ID = fmt.Sprintf("%d", old.ID)
	u.Username = old.Username
	u.Points = old.Points
	u.LastCheckin = old.LastCheckin
	u.IsAdmin = old.IsAdmin
	u.DisplayName = old.DisplayName
	u.Persona = old.Persona
	return nil
}

// Media represents a saved telegram photo or video.
type Media struct {
	ID        string   `json:"id"`
	FileID    string   `json:"file_id"`
	Type      string   `json:"type"` // "photo" or "video"
	Caption   string   `json:"caption"`
	AddedBy   string   `json:"added_by"`
	CreatedAt int64    `json:"created_at"`
	R2Key     string   `json:"r2_key,omitempty"` // Key in R2 bucket if uploaded
	Tags      []string `json:"tags,omitempty"`
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
		if _, e := tx.CreateBucketIfNotExists([]byte(imagesBucket)); e != nil {
			return e
		}
		if _, e := tx.CreateBucketIfNotExists([]byte(subsBucket)); e != nil {
			return e
		}
		if _, e := tx.CreateBucketIfNotExists([]byte(historyBucket)); e != nil {
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

// GetOrCreateUser retrieves or creates a user.
func (s *Store) GetOrCreateUser(id string, username, displayName string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		key := []byte(id)
		data := bucket.Get(key)
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

// AddPoints adds points to a user.
func (s *Store) AddPoints(id string, delta int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		key := []byte(id)
		data := bucket.Get(key)
		if data == nil {
			return fmt.Errorf("user %s not found", id)
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
func (s *Store) SetPoints(id string, points int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		key := []byte(id)
		data := bucket.Get(key)
		if data == nil {
			return fmt.Errorf("user %s not found", id)
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

// PromoteAdmin promotes a user to admin.
func (s *Store) PromoteAdmin(id string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		key := []byte(id)
		data := bucket.Get(key)
		if data == nil {
			return fmt.Errorf("user %s not found", id)
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
func (s *Store) CheckIn(id string, reward int) (int, *User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	var gained int
	err := s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(usersBucket))
		key := []byte(id)
		data := bucket.Get(key)
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

// ListUsers retrieves all users sorted by ID with pagination.
func (s *Store) ListUsers(limit, offset int) ([]User, error) {
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

	if offset >= len(users) {
		return []User{}, nil
	}
	end := offset + limit
	if end > len(users) {
		end = len(users)
	}
	return users[offset:end], nil
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

// SetRateLimit updates the allowed chat requests per minute. A value <=0 disables the limit.
func (s *Store) SetRateLimit(limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(settingsBucket))
		return bucket.Put([]byte(rateLimitKey), []byte(fmt.Sprintf("%d", limit)))
	})
}

// GetRateLimit returns the stored chat rate limit per minute and whether it was set.
// A stored value <=0 means the rate limit is disabled.
func (s *Store) GetRateLimit() (limit int, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err = s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(settingsBucket))
		value := bucket.Get([]byte(rateLimitKey))
		if value == nil {
			return nil
		}
		ok = true
		_, parseErr := fmt.Sscanf(string(value), "%d", &limit)
		return parseErr
	})
	return
}

// GetMedia returns a single media item by its ID.
func (s *Store) GetMedia(id string) (*Media, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var media Media
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(imagesBucket))
		val := bucket.Get([]byte(id))
		if val == nil {
			return errors.New("media not found")
		}
		return json.Unmarshal(val, &media)
	})
	if err != nil {
		return nil, err
	}
	return &media, nil
}

// SaveMedia saves a media item.
func (s *Store) SaveMedia(id, paramsType, caption string, addedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m := Media{
		ID:        id,
		FileID:    id, // Assuming ID is FileID for now
		Type:      paramsType,
		Caption:   caption,
		AddedBy:   addedBy,
		CreatedAt: time.Now().Unix(),
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(imagesBucket))
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(id), data)
	})
}

// SetMediaR2 updates the R2Key for a media item.
func (s *Store) SetMediaR2(id, r2Key string) error {
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
		m.R2Key = r2Key

		newData, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(id), newData)
	})
}

// GetRandomMedia returns a random media from the store.
func (s *Store) GetRandomMedia() (*Media, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var list []Media
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(imagesBucket))
		return bucket.ForEach(func(_, v []byte) error {
			var m Media
			if err := json.Unmarshal(v, &m); err == nil {
				list = append(list, m)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil // No media
	}

	randomIndex := time.Now().UnixNano() % int64(len(list))
	return &list[randomIndex], nil
}

// ListMedia retrieves all media with pagination.
func (s *Store) ListMedia(limit, offset int) ([]Media, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var list []Media
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(imagesBucket))
		return bucket.ForEach(func(_, v []byte) error {
			var m Media
			if err := json.Unmarshal(v, &m); err != nil {
				return err
			}
			list = append(list, m)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	// Sort by CreatedAt desc
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt > list[j].CreatedAt })

	if offset >= len(list) {
		return []Media{}, nil
	}
	end := offset + limit
	if end > len(list) {
		end = len(list)
	}
	return list[offset:end], nil
}

// CountMedia returns the total number of media items.
func (s *Store) CountMedia() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(imagesBucket))
		count = bucket.Stats().KeyN
		return nil
	})
	return count, err
}

// DeleteMedia removes a media by ID.
func (s *Store) DeleteMedia(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(imagesBucket))
		return bucket.Delete([]byte(id))
	})
}

// Subscribe adds a chat ID to subscriptions
func (s *Store) Subscribe(chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(subsBucket))
		// We use the chatID as the key.
		return b.Put([]byte(chatID), []byte("1"))
	})
}

// Unsubscribe removes a chat ID
func (s *Store) Unsubscribe(chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(subsBucket))
		return b.Delete([]byte(chatID))
	})
}

// ListSubscribers returns all subscriber IDs
func (s *Store) ListSubscribers() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(subsBucket))
		return b.ForEach(func(k, v []byte) error {
			ids = append(ids, string(k))
			return nil
		})
	})
	return ids, err
}

func saveUser(bucket *bbolt.Bucket, user *User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return bucket.Put([]byte(user.ID), data)
}
