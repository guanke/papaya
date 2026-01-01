package store

import (
	"encoding/json"
	"testing"
)

func TestUser_UnmarshalJSON_BackwardCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantID   string
		wantName string
	}{
		{
			name:     "old format with int64 ID",
			json:     `{"id":123456789,"username":"testuser","points":100,"last_checkin":"2026-01-01","is_admin":false,"display_name":"Test User"}`,
			wantID:   "123456789",
			wantName: "testuser",
		},
		{
			name:     "new format with string ID",
			json:     `{"id":"987654321","username":"newuser","points":50,"last_checkin":"2026-01-02","is_admin":true,"display_name":"New User"}`,
			wantID:   "987654321",
			wantName: "newuser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var user User
			err := json.Unmarshal([]byte(tt.json), &user)
			if err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}
			if user.ID != tt.wantID {
				t.Errorf("got ID = %q, want %q", user.ID, tt.wantID)
			}
			if user.Username != tt.wantName {
				t.Errorf("got Username = %q, want %q", user.Username, tt.wantName)
			}
		})
	}
}
