package config

import "testing"

func TestDefaultConfigUsesSingleHTTPListen(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.HTTP.Listen != ":23198" {
		t.Fatalf("unexpected http listen: %q", cfg.Server.HTTP.Listen)
	}
}

func TestValidateRejectsMissingHTTPListen(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.HTTP.Listen = " "

	if _, err := Validate(cfg); err != ErrInvalidHTTP {
		t.Fatalf("expected invalid http config, got %v", err)
	}
}

func TestValidateRejectsInvalidAuth(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ServerHTTPAuth)
	}{
		{name: "http issuer", mutate: func(cfg *ServerHTTPAuth) { cfg.Issuer = "http://auth.example.com" }},
		{name: "missing client", mutate: func(cfg *ServerHTTPAuth) { cfg.ClientID = "" }},
		{name: "remote http callback", mutate: func(cfg *ServerHTTPAuth) { cfg.CallbackURL = "http://tool.example.com/auth/callback" }},
		{name: "public URL path", mutate: func(cfg *ServerHTTPAuth) { cfg.PublicURL = "https://tool.example.com/app" }},
		{name: "callback host mismatch", mutate: func(cfg *ServerHTTPAuth) { cfg.PublicURL = "http://127.0.0.1:23199" }},
		{name: "non-numeric administrator ID", mutate: func(cfg *ServerHTTPAuth) { cfg.AdministratorIDs = []string{"lwmacct"} }},
		{name: "missing administrators", mutate: func(cfg *ServerHTTPAuth) {
			cfg.AdministratorIDs = nil
			cfg.AdministratorNames = nil
		}},
		{name: "invalid max age", mutate: func(cfg *ServerHTTPAuth) { cfg.MaxSessionAge = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig()
			test.mutate(&cfg.Server.HTTP.Auth)
			if _, err := Validate(cfg); err != ErrInvalidAuth {
				t.Fatalf("expected invalid auth config, got %v", err)
			}
		})
	}
}

func TestValidateNormalizesAuth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.HTTP.Auth.Issuer += "/"
	cfg.Server.HTTP.Auth.AdministratorNames = []string{" LwMacct "}

	validated, err := Validate(cfg)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if validated.Server.HTTP.Auth.Issuer != "https://2008.s.lwmacct.com:20088" {
		t.Fatalf("unexpected issuer: %q", validated.Server.HTTP.Auth.Issuer)
	}
	if validated.Server.HTTP.Auth.AdministratorNames[0] != "lwmacct" {
		t.Fatalf("unexpected username: %q", validated.Server.HTTP.Auth.AdministratorNames[0])
	}
}
