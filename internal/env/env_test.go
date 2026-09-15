package env

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Conf
		wantErr bool
	}{
		{"empty", "", Conf{}, false},
		{"comments and blanks", "# c\n\n  # indented\nA=1\n", Conf{"A": "1"}, false},
		{"value with equals", "K=a=b\n", Conf{"K": "a=b"}, false},
		{"empty value", "K=\n", Conf{"K": ""}, false},
		{"surrounding space", "  K=v  \n", Conf{"K": "v"}, false},
		{"later wins", "K=1\nK=2\n", Conf{"K": "2"}, false},
		{"no equals", "JUNK\n", nil, true},
		{"empty key", "=v\n", nil, true},
		{"space in key", "A B=1\n", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(strings.NewReader(tt.in), "test")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !maps.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGet(t *testing.T) {
	c := Conf{"A": "1", "E": ""}
	for _, tt := range []struct{ key, want string }{{"A", "1"}, {"E", "def"}, {"missing", "def"}} {
		if got := c.Get(tt.key, "def"); got != tt.want {
			t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

// TestLocalConf keeps env/local/env.conf, which the shell scripts source,
// parseable here and carrying the keys the binaries read.
func TestLocalConf(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", Path("local")))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"NS_A", "NS_B", "ADDR_A", "ADDR_B", "MTU", "PORT", "MEMSRV_MB"} {
		if c[k] == "" {
			t.Errorf("env/local/env.conf: %s missing", k)
		}
	}
}
