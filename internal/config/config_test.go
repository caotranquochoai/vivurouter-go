package config

import "testing"

func TestValidateDeploymentMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{name: "loopback defaults local", cfg: Config{Host: "127.0.0.1"}, want: true},
		{name: "non-loopback requires explicit mode", cfg: Config{Host: "0.0.0.0"}, want: false},
		{name: "private non-loopback allowed", cfg: Config{Host: "0.0.0.0", DeploymentMode: DeploymentPrivate}, want: true},
		{name: "local non-loopback rejected", cfg: Config{Host: "0.0.0.0", DeploymentMode: DeploymentLocal}, want: false},
		{name: "unknown mode rejected", cfg: Config{Host: "127.0.0.1", DeploymentMode: "internet"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateDeploymentMode()
			if (err == nil) != tt.want {
				t.Fatalf("ValidateDeploymentMode() error = %v, want success=%t", err, tt.want)
			}
		})
	}
}

func TestEffectiveDeploymentMode(t *testing.T) {
	if got := (Config{Host: "::1"}).EffectiveDeploymentMode(); got != DeploymentLocal {
		t.Fatalf("loopback effective mode = %q, want %q", got, DeploymentLocal)
	}
	if got := (Config{Host: "192.168.1.5"}).EffectiveDeploymentMode(); got != "" {
		t.Fatalf("non-loopback effective mode = %q, want empty", got)
	}
}
