package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/yesdevnull/trenchcoat/internal/config"
)

func TestLoad_ExplicitPath(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("port: 9090\nwatch: true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if viper.GetInt("port") != 9090 {
		t.Fatalf("expected port 9090, got %d", viper.GetInt("port"))
	}
	if !viper.GetBool("watch") {
		t.Fatal("expected watch to be true")
	}
}

func TestLoad_NoConfigFile(t *testing.T) {
	viper.Reset()
	// Change to a temp dir with no config file.
	dir := t.TempDir()
	t.Chdir(dir)

	err := config.Load("")
	if err != nil {
		t.Fatalf("expected no error when no config file exists, got: %v", err)
	}
}

func TestLoad_CwdConfig(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, ".trenchcoat.yaml")
	if err := os.WriteFile(cfgFile, []byte("port: 3000\nlog_format: json\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	err := config.Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if viper.GetInt("port") != 3000 {
		t.Fatalf("expected port 3000, got %d", viper.GetInt("port"))
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("invalid: yaml: [unterminated"), 0644); err != nil {
		t.Fatal(err)
	}

	err := config.Load(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid YAML config file")
	}
}

func TestLoad_CwdConfig_YmlExtension(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, ".trenchcoat.yml")
	if err := os.WriteFile(cfgFile, []byte("port: 4000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	err := config.Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if viper.GetInt("port") != 4000 {
		t.Fatalf("expected port 4000, got %d", viper.GetInt("port"))
	}
}

func TestLoad_NestedConfig(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte(`
tls:
  cert: ./certs/server.pem
  key: ./certs/server-key.pem
proxy:
  write_dir: ./captured
  dedupe: skip
`), 0644); err != nil {
		t.Fatal(err)
	}

	err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if viper.GetString("tls.cert") != "./certs/server.pem" {
		t.Fatalf("expected tls.cert to be ./certs/server.pem, got %q", viper.GetString("tls.cert"))
	}
	if viper.GetString("proxy.dedupe") != "skip" {
		t.Fatalf("expected proxy.dedupe to be skip, got %q", viper.GetString("proxy.dedupe"))
	}
}
