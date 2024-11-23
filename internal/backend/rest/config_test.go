package rest

import (
	"net/url"
	"os"
	"testing"

	"github.com/restic/restic/internal/backend/test"
)

func parseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}

	return u
}

func TestParseConfig(t *testing.T) {
	configTests := []test.ConfigTestData[Config]{
		{
			S: "rest:http://localhost:1234",
			Cfg: Config{
				URL:         parseURL("http://localhost:1234/"),
				Connections: 5,
			},
		},
		{
			S: "rest:http://localhost:1234/",
			Cfg: Config{
				URL:         parseURL("http://localhost:1234/"),
				Connections: 5,
			},
		},
		{
			S: "rest:http+unix:///tmp/rest.socket:/my_backup_repo/",
			Cfg: Config{
				URL:         parseURL("http+unix:///tmp/rest.socket:/my_backup_repo/"),
				Connections: 5,
			},
		},
	}

	test.ParseConfigTester(t, ParseConfig, configTests)
}

func TestStripPassword(t *testing.T) {
	passwordTests := []struct {
		input    string
		expected string
	}{
		{
			"rest:",
			"rest:/",
		},
		{
			"rest:localhost/",
			"rest:localhost/",
		},
		{
			"rest::123/",
			"rest::123/",
		},
		{
			"rest:http://",
			"rest:http://",
		},
		{
			"rest:http://hostname.foo:1234/",
			"rest:http://hostname.foo:1234/",
		},
		{
			"rest:http://user@hostname.foo:1234/",
			"rest:http://user@hostname.foo:1234/",
		},
		{
			"rest:http://user:@hostname.foo:1234/",
			"rest:http://user:***@hostname.foo:1234/",
		},
		{
			"rest:http://user:p@hostname.foo:1234/",
			"rest:http://user:***@hostname.foo:1234/",
		},
		{
			"rest:http://user:pppppaaafhhfuuwiiehhthhghhdkjaoowpprooghjjjdhhwuuhgjsjhhfdjhruuhsjsdhhfhshhsppwufhhsjjsjs@hostname.foo:1234/",
			"rest:http://user:***@hostname.foo:1234/",
		},
		{
			"rest:http://user:password@hostname",
			"rest:http://user:***@hostname/",
		},
		{
			"rest:http://user:password@:123",
			"rest:http://user:***@:123/",
		},
		{
			"rest:http://user:password@",
			"rest:http://user:***@/",
		},
	}

	// Make sure that the factory uses the correct method
	StripPassword := NewFactory().StripPassword

	for i, test := range passwordTests {
		t.Run(test.input, func(t *testing.T) {
			result := StripPassword(test.input)
			if result != test.expected {
				t.Errorf("test %d: expected '%s' but got '%s'", i, test.expected, result)
			}
		})
	}
}

func TestPasswordPriority(t *testing.T) {
	const (
		pwdClearValue = "clear value"
		pwdFileValue  = "file value"
	)

	tmpFile, err := os.CreateTemp("", "")
	if err != nil {
		t.Fatalf("couldn't create temporary file")
	}
	_, err = tmpFile.Write([]byte(pwdFileValue))
	if err != nil {
		t.Fatalf("couldn't write content into temporary file")
	}
	defer os.Remove(tmpFile.Name())

	combinations := []struct {
		WithClear bool
		WithFile  bool
		Value     string
	}{
		{false, false, ""},
		{false, true, pwdFileValue},
		{true, false, pwdClearValue},
		{true, true, pwdClearValue},
	}
	for _, comb := range combinations {
		if comb.WithClear {
			os.Setenv("TEST_"+EnvPasswordCleartext, pwdClearValue)
		} else {
			os.Unsetenv("TEST_" + EnvPasswordCleartext)
		}
		if comb.WithFile {
			os.Setenv("TEST_"+EnvPasswordFromFile, tmpFile.Name())
		} else {
			os.Unsetenv("TEST_" + EnvPasswordFromFile)
		}
		conf := Config{URL: parseURL("rest:http://hostname.foo:1234/")}
		conf.ApplyEnvironment("TEST_")
		if got, _ := conf.URL.User.Password(); got != comb.Value {
			t.Fatalf("expected password value %q. Got %q", comb.Value, got)
		}
	}
}
