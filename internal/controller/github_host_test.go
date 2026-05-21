package controller

import "testing"

func TestResolveGitHubHost(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantHost   string
		expectFail bool
	}{
		{
			name:     "empty defaults to github.com",
			input:    "",
			wantHost: defaultGitHubHost,
		},
		{
			name:     "plain github.com host",
			input:    "github.com",
			wantHost: "github.com",
		},
		{
			name:     "github.com with scheme",
			input:    "https://github.com",
			wantHost: "github.com",
		},
		{
			name:     "enterprise host",
			input:    "ghe.example.com",
			wantHost: "ghe.example.com",
		},
		{
			name:     "enterprise host with scheme and port",
			input:    "https://ghe.example.com:8443",
			wantHost: "ghe.example.com:8443",
		},
		{
			name:       "path is invalid",
			input:      "ghe.example.com/api/v3",
			expectFail: true,
		},
		{
			name:       "malformed URL is invalid",
			input:      "https://",
			expectFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveGitHubHost(tt.input)
			if tt.expectFail {
				if err == nil {
					t.Fatalf("expected an error for input %q", tt.input)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if got != tt.wantHost {
				t.Fatalf("resolveGitHubHost(%q) = %q, want %q", tt.input, got, tt.wantHost)
			}
		})
	}
}

func TestGitHubAPIBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "github.com maps to public api host",
			host:     "github.com",
			expected: "https://api.github.com",
		},
		{
			name:     "api.github.com preserved",
			host:     "api.github.com",
			expected: "https://api.github.com",
		},
		{
			name:     "enterprise host maps to api v3 path",
			host:     "ghe.example.com",
			expected: "https://ghe.example.com/api/v3",
		},
		{
			name:     "enterprise host with port maps to api v3 path",
			host:     "ghe.example.com:8443",
			expected: "https://ghe.example.com:8443/api/v3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := githubAPIBaseURL(tt.host); got != tt.expected {
				t.Fatalf("githubAPIBaseURL(%q) = %q, want %q", tt.host, got, tt.expected)
			}
		})
	}
}
