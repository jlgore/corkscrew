package db

import "testing"

func TestParseTarget_Local(t *testing.T) {
	cases := []string{
		"/home/user/.corkscrew/db/corkscrew.duckdb",
		"corkscrew.duckdb",
		"./relative/path.db",
		"", // in-memory
	}
	for _, c := range cases {
		got, err := ParseTarget(c)
		if err != nil {
			t.Fatalf("ParseTarget(%q) unexpected error: %v", c, err)
		}
		if got.IsRemote {
			t.Errorf("ParseTarget(%q).IsRemote = true, want false", c)
		}
		if got.Endpoint != "" {
			t.Errorf("ParseTarget(%q).Endpoint = %q, want empty", c, got.Endpoint)
		}
	}
}

func TestParseTarget_Remote(t *testing.T) {
	cases := []struct {
		in          string
		endpoint    string
		attachURI   string
		disableSSL  bool
		token       string
	}{
		{in: "quack:localhost", endpoint: "localhost", attachURI: "quack:localhost"},
		{in: "quack:host:9494", endpoint: "host:9494", attachURI: "quack:host:9494"},
		{in: "quack:[::1]:1234", endpoint: "[::1]:1234", attachURI: "quack:[::1]:1234"},
		{in: "quack://remote.example.com:9000", endpoint: "remote.example.com:9000", attachURI: "quack:remote.example.com:9000"},
		{in: "quack:host:9494?disable_ssl=true", endpoint: "host:9494", attachURI: "quack:host:9494", disableSSL: true},
		{in: "quack:host?disable_ssl=1", endpoint: "host", attachURI: "quack:host", disableSSL: true},
		{in: "quack:host?token=abc123", endpoint: "host", attachURI: "quack:host", token: "abc123"},
		{in: "quack:host:9494?token=t&disable_ssl=yes", endpoint: "host:9494", attachURI: "quack:host:9494", token: "t", disableSSL: true},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if err != nil {
			t.Fatalf("ParseTarget(%q) unexpected error: %v", c.in, err)
		}
		if !got.IsRemote {
			t.Errorf("ParseTarget(%q).IsRemote = false, want true", c.in)
		}
		if got.Endpoint != c.endpoint {
			t.Errorf("ParseTarget(%q).Endpoint = %q, want %q", c.in, got.Endpoint, c.endpoint)
		}
		if got.AttachURI() != c.attachURI {
			t.Errorf("ParseTarget(%q).AttachURI() = %q, want %q", c.in, got.AttachURI(), c.attachURI)
		}
		if got.DisableSSL != c.disableSSL {
			t.Errorf("ParseTarget(%q).DisableSSL = %v, want %v", c.in, got.DisableSSL, c.disableSSL)
		}
		if got.Token != c.token {
			t.Errorf("ParseTarget(%q).Token = %q, want %q", c.in, got.Token, c.token)
		}
	}
}

func TestParseTarget_Invalid(t *testing.T) {
	cases := []string{
		"quack:",            // missing host
		"quack://",          // missing host after authority
		"quack:ho st",       // whitespace in host
		"quack:h'ost",       // single quote (SQL injection guard)
	}
	for _, c := range cases {
		if _, err := ParseTarget(c); err == nil {
			t.Errorf("ParseTarget(%q) expected error, got nil", c)
		}
	}
}

func TestIsRemoteTarget(t *testing.T) {
	if !IsRemoteTarget("quack:localhost") {
		t.Error("IsRemoteTarget(quack:localhost) = false, want true")
	}
	if IsRemoteTarget("/path/to.duckdb") {
		t.Error("IsRemoteTarget(/path/to.duckdb) = true, want false")
	}
}

func TestBuildAttachStatement(t *testing.T) {
	cases := []struct {
		name string
		t    Target
		o    connectOptions
		want string
	}{
		{
			name: "no options",
			t:    Target{Endpoint: "localhost"},
			want: "ATTACH IF NOT EXISTS 'quack:localhost' AS corkscrew_remote;",
		},
		{
			name: "token only",
			t:    Target{Endpoint: "host:9494"},
			o:    connectOptions{token: "tok"},
			want: "ATTACH IF NOT EXISTS 'quack:host:9494' AS corkscrew_remote (TOKEN 'tok');",
		},
		{
			name: "token and disable_ssl",
			t:    Target{Endpoint: "host"},
			o:    connectOptions{token: "tok", disableSSL: true},
			want: "ATTACH IF NOT EXISTS 'quack:host' AS corkscrew_remote (TOKEN 'tok', DISABLE_SSL true);",
		},
		{
			name: "token with embedded quote is escaped",
			t:    Target{Endpoint: "host"},
			o:    connectOptions{token: "a'b"},
			want: "ATTACH IF NOT EXISTS 'quack:host' AS corkscrew_remote (TOKEN 'a''b');",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildAttachStatement(c.t, c.o); got != c.want {
				t.Errorf("buildAttachStatement() = %q, want %q", got, c.want)
			}
		})
	}
}
