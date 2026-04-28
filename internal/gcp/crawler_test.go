package gcp

import "testing"

func TestFolderResourceID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"bare empty", "", "", false},
		{"folders id", "folders/123456789", "123456789", true},
		{"org", "organizations/123", "", false},
		{"folders trailing", "folders/", "", false},
		{"not prefix", "123456789", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := folderResourceID(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("folderResourceID(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestExcludedHas(t *testing.T) {
	ex := map[string]struct{}{"111": {}, "222": {}}
	if !excludedHas(ex, "111") {
		t.Fatal("bare id 111 should match")
	}
	if !excludedHas(ex, "folders/222") {
		t.Fatal("folders/222 should match 222")
	}
	if excludedHas(ex, "folders/333") {
		t.Fatal("333 should not match")
	}
	if excludedHas(nil, "folders/111") {
		t.Fatal("nil excluded should not match")
	}
	if excludedHas(map[string]struct{}{}, "111") {
		t.Fatal("empty excluded should not match")
	}
}
