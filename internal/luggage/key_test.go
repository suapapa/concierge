package luggage

import (
	"errors"
	"testing"
)

func TestValidateKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		key     string
		wantErr error
	}{
		{name: "empty", key: "", wantErr: ErrInvalidKey},
		{name: "hex", key: "a1b2c3d4e5f6789012345678abcdef01", wantErr: nil},
		{name: "underscore", key: "my_key_1", wantErr: nil},
		{name: "dotdot", key: "x..y", wantErr: ErrInvalidKey},
		{name: "slash", key: "a/b", wantErr: ErrInvalidKey},
		{name: "space", key: "a b", wantErr: ErrInvalidKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateKey(tt.key)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateKey(%q) = %v; want nil", tt.key, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateKey(%q) = %v; want wrap %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestGenerateKey_unique(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	for range 64 {
		k, err := generateKey()
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateKey(k); err != nil {
			t.Fatalf("generated key invalid: %v", err)
		}
		if _, ok := seen[k]; ok {
			t.Fatalf("duplicate key %q", k)
		}
		seen[k] = struct{}{}
	}
}
