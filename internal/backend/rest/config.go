package rest

import (
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/restic/restic/internal/backend"
	"github.com/restic/restic/internal/errors"
	"github.com/restic/restic/internal/options"
)

// Config contains all configuration necessary to connect to a REST server.
type Config struct {
	URL         *url.URL
	Connections uint `option:"connections" help:"set a limit for the number of concurrent connections (default: 5)"`
}

func init() {
	options.Register("rest", Config{})
}

// NewConfig returns a new Config with the default values filled in.
func NewConfig() Config {
	return Config{
		Connections: 5,
	}
}

// ParseConfig parses the string s and extracts the REST server URL.
func ParseConfig(s string) (*Config, error) {
	if !strings.HasPrefix(s, "rest:") {
		return nil, errors.New("invalid REST backend specification")
	}

	s = prepareURL(s)

	u, err := url.Parse(s)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	cfg := NewConfig()
	cfg.URL = u
	return &cfg, nil
}

// StripPassword removes the password from the URL
// If the repository location cannot be parsed as a valid URL, it will be returned as is
// (it's because this function is used for logging errors)
func StripPassword(s string) string {
	scheme := s[:5]
	s = prepareURL(s)

	u, err := url.Parse(s)
	if err != nil {
		return scheme + s
	}

	if _, set := u.User.Password(); !set {
		return scheme + s
	}

	// a password was set: we replace it with ***
	return scheme + strings.Replace(u.String(), u.User.String()+"@", u.User.Username()+":***@", 1)
}

func prepareURL(s string) string {
	s = s[5:]
	if !strings.HasSuffix(s, "/") {
		s += "/"
	}
	return s
}

var _ backend.ApplyEnvironmenter = &Config{}

const (
	// EnvPasswordCleartext is the environment variable
	// that specifies user's password for the REST backend
	// when it's not embedded in the URL.
	EnvPasswordCleartext = "RESTIC_REST_PASSWORD"

	// EnvPasswordFromFile is the environment variable
	// that specifies the file path containing user's password
	// for the REST backend when it's not embedded in the URL.
	EnvPasswordFromFile = "RESTIC_REST_PASSWORD_FILE"
)

// ApplyEnvironment saves values from the environment to the config.
func (cfg *Config) ApplyEnvironment(prefix string) {
	username := cfg.URL.User.Username()
	_, pwdSet := cfg.URL.User.Password()

	// Only apply env variable values if neither username nor password are provided.
	if username == "" && !pwdSet {
		envName := os.Getenv(prefix + "RESTIC_REST_USERNAME")
		envPwd, pwdSet := os.LookupEnv(prefix + EnvPasswordCleartext)
		if !pwdSet {
			filePath, pathSet := os.LookupEnv(prefix + EnvPasswordFromFile)
			if pathSet {
				envPwd, _ = readPasswordFromFile(filePath)
			}
		}

		cfg.URL.User = url.UserPassword(envName, envPwd)
	}
}

func readPasswordFromFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var builder strings.Builder
	// Prevent big files to hang the application.
	// Passwords longer than 1 KiB are very unlikely anyway.
	_, err = io.Copy(&builder, io.LimitReader(f, 1024))
	return builder.String(), err
}
